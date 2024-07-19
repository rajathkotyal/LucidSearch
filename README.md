A query engine initiative to reduce AI Hallucinations and provide concrete insights.

Hallucinations is one of the key drawbacks of current LLMs, especially for important use cases like finding legal / medical / scientific information online. Additionally, Current LLMs have a generic and defined search space mostly made of text.

This is a simple service aimed to reduce/eliminate hallucinations by utilizing mp3, mp4 and text using Retrieval Augmented Generation (RAG) : 

--> User queries the engine, it returns the response with references to where it got the information from.

Current implementation : (I built the entire service in Go, mainly due to its robust support for concurrent and async processing)

Workflow: (Context can contain audio - podcasts, video - currently only from TedTalks and text inputs) :

1. Information is extracted from major trusted websites (.gov, .edu etc..)
2. Audio transcripts are fetched from related podcasts and videos (TedTalks pre cached in an in-memory DB).
3. Vector embeddings are calculated and the values are stored in a Qdrant vector DB.
4. Query vector is calculated and searched in the vector store for similar entities.
5. Results are fed to a fine tuned Gemma-7b LLM (Local instance) and returned.
6. The response has references embedded into it such that the user can verify the claim and backtrace it for explainability

How to run : 
It is a prelimnary test project, havent included "go-to" runner script yet. Feel free to email at rajathkotyal@gmail.com if you want to try it out.
