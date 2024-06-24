package extract

import (
	"fmt"
	"log"
	"lucidsearch/embedstore"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/tmc/langchaingo/schema"

	"github.com/PuerkitoBio/goquery"
	readability "github.com/go-shiori/go-readability"
	"github.com/texttheater/golang-levenshtein/levenshtein"
)

const (
	googleSearchURL = "https://www.googleapis.com/customsearch/v1"
	geminiAPIURL    = "https://api.gemini.com/v1/embedding"
)

func Scrape(result embedstore.Result, tedTalks []TEDTalk) (string, error) {
	if result.Link == "" {
		return "", nil
	}

	if result.IsTED {
		fmt.Println("Ted talk scraping")
		s := scrapeTedUrl(result, tedTalks)
		fmt.Println("FETCHED FROM ted DB : ", s)
		return s, nil
	} else {
		return fetchURLContent(result.Link, 3, 1*time.Second, 5*time.Second)
	}
}

func scrapeTedUrl(result embedstore.Result, tedTalks []TEDTalk) string {
	req, err := http.NewRequest("GET", result.Link, nil)
	if err != nil {
		log.Println("Error creating request:", err)
		return ""
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Error fetching the URL:", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("Non-OK HTTP status:", resp)
		return ""
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Println("Error loading HTML document:", err)
		return ""
	}

	fmt.Println("Ted talk scraping")
	s := scrapeTEDTalk(doc, tedTalks)
	fmt.Println("FETCHED FROM ted DB : ", s)
	return s

}

func ExtractContent(doc *goquery.Document) string {
	selectors := []string{
		"article",
		".content",
		".main",
		".post",
		".entry-content",
		".blog-post",
		".single-post",
		".article-content",
		"#content",
		"#main",
	}

	for _, selector := range selectors {
		selection := doc.Find(selector)
		if selection.Length() > 0 {
			return selection.Text()
		}
	}

	// Fallback
	log.Println("No content found with the provided selectors, extracting from body tag")
	bodyContent := doc.Find("body").Text()
	return bodyContent
}

func scrapeWebPage(doc *goquery.Document) string {
	content := ExtractContent(doc)
	text := cleanText(content)
	return text
}

func fetchURLContent(url string, maxRetries int, retryDelay time.Duration, timeout time.Duration) (string, error) {
	log.Printf("Scraping content from URL: %s", url)
	retries := 0

	client := &http.Client{
		Timeout: timeout,
	}

	for retries < maxRetries {
		resp, err := client.Get(url)
		if err != nil {
			log.Printf("Error occurred while scraping URL: %s. Error: %v", url, err)
			retries++
			time.Sleep(retryDelay)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			doc, err := goquery.NewDocumentFromReader(resp.Body)
			if err != nil {
				log.Printf("Error parsing document from URL: %s. Error: %v", url, err)
				return "", err
			}

			selectors := []string{
				"article",
				"div.main-content",
				"body",
			}

			var mainText string
			for _, selector := range selectors {
				doc.Find(selector).Each(func(i int, s *goquery.Selection) {
					text := cleanText(s.Text())
					if text != "" {
						mainText += text + "\n"
					}
				})
				if mainText != "" {
					break
				}
			}

			if strings.TrimSpace(mainText) == "" {
				log.Printf("No content extracted from URL: %s", url)
			}
			return mainText, nil
		} else {
			log.Printf("Request failed with status code: %d", resp.StatusCode)
			return "", fmt.Errorf("request failed with status code: %d", resp.StatusCode)
		}
	}

	log.Printf("Failed to scrape content from URL: %s after %d retries.", url, maxRetries)
	return "", fmt.Errorf("failed to scrape content from URL: %s after %d retries", url, maxRetries)
}

func cleanText(text string) string {

	// text = stripHTMLTags(text)

	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	return text
}

func stripHTMLTags(text string) string {
	re := regexp.MustCompile("<.*?>")
	return re.ReplaceAllString(text, "")
}

type TEDTalk struct {
	Title   string `json:"title"`
	Output  string `json:"output"`
	Speaker string `json:"speaker"`
}

func isTEDTalk(url string) bool {
	if strings.Contains(url, "ted.com") {
		return true
	}

	validPrefixes := []string{
		"http://www.ted.com/talks/",
		"https://www.ted.com/talks/",
		"http://ted.com/talks/",
		"https://ted.com/talks/",
	}

	for _, prefix := range validPrefixes {
		if strings.HasPrefix(url, prefix) {
			return true
		}
	}

	return false
}

func Similarity(a, b string) float64 {
	distance := levenshtein.DistanceForStrings([]rune(a), []rune(b), levenshtein.DefaultOptions)
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	return (1 - float64(distance)/float64(maxLen)) * 100
}

func SearchTEDTalk(title, speaker string, tedTalks []TEDTalk) string {
	for _, talk := range tedTalks {
		s := Similarity(talk.Title, title)
		if s >= 70 {
			fmt.Print("Similarity bw : ", talk.Title, title, "is ", s)
			return fmt.Sprintf("%s", talk.Output)
		}
	}
	return "No matching TED Talk found."
}

func scrapeTEDTalk(doc *goquery.Document, tedTalks []TEDTalk) string {
	title := doc.Find("meta[property='og:title']").AttrOr("content", "No title found")
	speaker := doc.Find(".talk-speaker__name").Text()

	// fmt.Print("TED DETAILS : ", title, "XXXX ", speaker)

	if title != "" || speaker != "" {
		return SearchTEDTalk(title, speaker, tedTalks)
	} else {
		return "Could not extract TED Talk details."
	}
}

// TODO
func scrapeAndParse(url string) (schema.Document, error) {
	// resp, err := http.Get(url)
	// if err != nil {
	// 	return schema.Document{}, err
	// }
	// defer resp.Body.Close()

	// body, err := ioutil.ReadAll(resp.Body)
	// if err != nil {
	// 	return schema.Document{}, err
	// }


	article, err := readability.FromURL(url, 10*time.Second)
	if err != nil {
		log.Println("Error loading HTML document:", err)
		return schema.Document{}, nil
	}

	// Create a Document object
	return schema.Document{
		PageContent: article.Content,
		Metadata: map[string]any{
			"name": article.Title,
			"url":  url,
		},
	}, nil
}
