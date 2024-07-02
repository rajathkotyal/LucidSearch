package embedstore

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/generative-ai-go/genai"
	"github.com/google/uuid"
	pb "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func StoreInQdrant(title, link string, embedding []float32, chunkStr string) {
	conn, err := grpc.Dial("localhost:6334", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewPointsClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	id := uuid.NewString()

	payload := map[string]*pb.Value{
		"title": {Kind: &pb.Value_StringValue{StringValue: title}},
		"link":  {Kind: &pb.Value_StringValue{StringValue: link}},
		"text":  {Kind: &pb.Value_StringValue{StringValue: chunkStr}},
	}

	_, err = client.Upsert(ctx, &pb.UpsertPoints{
		CollectionName: "embeddings",
		Points: []*pb.PointStruct{
			{
				Id: &pb.PointId{
					PointIdOptions: &pb.PointId_Uuid{
						Uuid: id,
					},
				},
				Vectors: &pb.Vectors{
					VectorsOptions: &pb.Vectors_Vector{
						Vector: &pb.Vector{
							Data: embedding,
						},
					},
				},
				Payload: payload,
			},
		},
	})
	if err != nil {
		log.Fatalf("could not upsert point: %v", err)
	}
	// fmt.Printf("STORED: ID: %s, Payload: %+v\n", id, payload)
}

func SetupQdrantCollection(d int) {
	conn, err := grpc.Dial("localhost:6334", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectionsClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dimension := d

	_, _ = client.Delete(ctx, &pb.DeleteCollection{
		CollectionName: "embeddings",
	})

	_, err = client.Create(ctx, &pb.CreateCollection{
		CollectionName: "embeddings",
		VectorsConfig: &pb.VectorsConfig{
			Config: &pb.VectorsConfig_Params{
				Params: &pb.VectorParams{
					Size:     uint64(dimension),
					Distance: pb.Distance_Cosine,
				},
			},
		},
	})
	if err != nil {
		log.Fatalf("could not create collection: %v", err)
	}
}

func SearchQdrant(queryEmbedding []float32, limit int, scoreThreshold float32) ([]string, error) {
	conn, err := grpc.Dial("localhost:6334", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("did not connect: %w", err)
	}
	defer conn.Close()

	client := pb.NewPointsClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	searchResult, err := client.Search(ctx, &pb.SearchPoints{
		CollectionName: "embeddings",
		Vector:         queryEmbedding,
		Limit:          uint64(limit),
		WithPayload: &pb.WithPayloadSelector{
			SelectorOptions: &pb.WithPayloadSelector_Enable{
				Enable: true,
			},
		},
		ScoreThreshold: &scoreThreshold,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search Qdrant: %w", err)
	}

	var chunkIDs []string
	for _, result := range searchResult.Result {
		chunkIDs = append(chunkIDs, result.Id.GetUuid())
		// fmt.Println("SEARCH RESS : ", result.GetPayload(), result)
	}

	return chunkIDs, nil
}

type ChunkData struct {
	Title string
	Link  string
	Text  string
}

func GetChunks(chunkIDs []string) ([]ChunkData, error) {
	conn, err := grpc.Dial("localhost:6334", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("did not connect: %w", err)
	}
	defer conn.Close()

	client := pb.NewPointsClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var chunks []ChunkData
	for _, chunkID := range chunkIDs {
		pointID := &pb.PointId{
			PointIdOptions: &pb.PointId_Uuid{
				Uuid: chunkID,
			},
		}

		response, err := client.Get(ctx, &pb.GetPoints{
			CollectionName: "embeddings",
			Ids:            []*pb.PointId{pointID},
			WithPayload: &pb.WithPayloadSelector{
				SelectorOptions: &pb.WithPayloadSelector_Enable{
					Enable: true,
				},
			},
		})
		if err != nil {
			log.Printf("failed to retrieve point for chunk ID %s: %v", chunkID, err)
			continue
		}

		// fmt.Printf("Response for chunk ID %s: %+v\n", chunkID, response.GetResult())

		for _, point := range response.Result {
			payload := point.Payload

			if text, ok := payload["text"]; ok {
				textContent := text.GetStringValue()
				if !strings.Contains(textContent, "::") && !strings.Contains(textContent, "{") && !strings.Contains(textContent, "}") {
					cdata := ChunkData{
						Title: payload["title"].GetStringValue(),
						Link:  payload["link"].GetStringValue(),
						Text:  textContent,
					}
					chunks = append(chunks, cdata)
				} else {
					log.Printf("filtered out noisy text for chunk ID %s", chunkID)
				}
			} else {
				log.Printf("text field not found in payload for chunk ID %s", chunkID)
			}
		}
	}

	return chunks, nil
}

type Document struct {
	PageContent string
	Metadata    map[string]string
}

func ChunkToDocuments(chunks []ChunkData, maxTokens int) []Document {
	var documents []Document
	currentTokens := 0

	for _, chunk := range chunks {
		chunkTokens := len(strings.Fields(chunk.Text))
		if currentTokens+chunkTokens > maxTokens {
			break
		}
		document := Document{
			PageContent: chunk.Text,
			Metadata: map[string]string{
				"name": chunk.Title,
				"url":  chunk.Link,
			},
		}
		documents = append(documents, document)
		currentTokens += chunkTokens
	}

	log.Printf("Converted %d chunks into %d documents", len(chunks), len(documents))
	return documents
}

func SplitContentByBytes(content string, maxBytes int) []string {
	var chunks []string
	var currentChunk strings.Builder
	currentSize := 0

	words := strings.Fields(content)
	for _, word := range words {
		wordSize := utf8.RuneCountInString(word)
		if currentSize+wordSize+1 > maxBytes {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
				currentSize = 0
			}

			if wordSize > maxBytes {
				for start := 0; start < len(word); {
					end := start + maxBytes
					if end > len(word) {
						end = len(word)
					}
					chunks = append(chunks, word[start:end])
					start = end
				}
			} else {
				currentChunk.WriteString(word)
				currentSize = wordSize
			}
		} else {
			if currentChunk.Len() > 0 {
				currentChunk.WriteString(" ")
				currentSize++
			}
			currentChunk.WriteString(word)
			currentSize += wordSize
		}
	}
	if currentChunk.Len() > 0 && currentChunk.Len() < 9990 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}

func SanitizeUTF8(input string) string {
	if utf8.ValidString(input) {
		return input
	}
	v := make([]rune, 0, len(input))
	for i, r := range input {
		if r == utf8.RuneError {
			_, size := utf8.DecodeRuneInString(input[i:])
			if size == 1 {
				continue 
			}
		}
		v = append(v, r)
	}
	return string(v)
}

var totalChunks = 0

type Result struct {
	Title string `json:"title"`
	Link  string `json:"link"`
	IsTED bool   `json:"isted"`
}

func GetGeminiEmbedding(ctx context.Context, client *genai.Client, content, model, title string, result Result, isQuery bool) []float32 {
	const maxBytes = 9000
	const maxChunks = 5

	// var textsplitters textsplitter.TextSplitter
	// textsplitters = textsplitter.NewRecursiveCharacter(
	// 	textsplitter.WithChunkSize(1000),
	// 	textsplitter.WithChunkOverlap(200),
	// 	textsplitter.WithSeparators([]string{" "}),
	// )

	// splitDocuments, err := textsplitter.SplitDocuments(textsplitters, []schema.Document{document})
	// if err != nil {
	// 	fmt.Errorf("err")
	// }

	chunks := SplitContentByBytes(content, maxBytes)
	// fmt.Println("SPLIT DOCS  : ", chunks)
	processedChunks := 0
	// var combinedEmbedding []float32
	for _, chunk := range chunks {
		if processedChunks >= maxChunks {
			break
		}
		chunk = SanitizeUTF8(chunk)
		fmt.Printf("Processing chunk %d of size %d bytes\n", processedChunks, len(chunk))
		em := client.EmbeddingModel(model)
		res, err := em.EmbedContent(ctx, genai.Text(chunk))
		if err != nil {
			fmt.Printf("failed to generate embedding: %w", err)
			continue
		}
		if res.Embedding != nil && res.Embedding.Values != nil && chunk != "" {
			StoreInQdrant(result.Title, result.Link, res.Embedding.Values, chunk)
			processedChunks++
			totalChunks++
		}
		if isQuery {
			var combinedEmbedding []float32
			combinedEmbedding = append(combinedEmbedding, res.Embedding.Values...)
			return combinedEmbedding
		}

	}
	// fmt.Println("CHONSKSS: ", chunks)
	return nil

}
