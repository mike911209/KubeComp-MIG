package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

const url = "http://localhost:8080"

var (
	prompt       string
	maxNewTokens int
	showBody     bool
)

func init() {
	flag.StringVar(&prompt, "prompt", "What is Deep Learning?", "Prompt to send to the model")
	flag.IntVar(&maxNewTokens, "max_new_tokens", 1000, "Maximum number of tokens to generate")
	flag.BoolVar(&showBody, "show-body", false, "Show the body of the response")
	flag.Usage = usage
}

func usage() {
	fmt.Println("Usage: send [-prompt <string>] [-max_new_tokens <int>] [-show-body=true] <model> <number of requests>")
	fmt.Println("Arguments:")
	fmt.Println("  model: Model to use")
	fmt.Println("     0) Meta-Llama-3.1-8B")
	fmt.Println("     1) Llama-3.2-1B-Instruct")
	fmt.Println("     2) gpt2-small")
	fmt.Println("  number_of_requests: Number of requests to send")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -h, --help              Display this help message")
	flag.PrintDefaults()
}

func main() {
	flag.Parse()

	models := []string{
		"meta-llama/Meta-Llama-3.1-8B",
		"meta-llama/Llama-3.2-1B-Instruct",
		"openai-community/gpt2"}

	modelIdx, err := strconv.Atoi(flag.Arg(0))
	if err != nil {
		log.Fatal("Invalid model request")
		return
	}
	model := models[modelIdx]
	requests, err := strconv.Atoi(flag.Arg(1))
	if err != nil {
		log.Fatal("Invalid number of requests")
		return
	}

	fmt.Printf("Sending %d requests to model %s\n", requests, model)

	jsonData := []byte(`{
		"token": "` + prompt + `",
		"par": {
			"max_new_tokens": ` + strconv.Itoa(maxNewTokens) + `
		},
		"env": {
			"MODEL_ID": "` + model + `", 
			"HF_TOKEN": "` + os.Getenv("HF_TOKEN") + `"
		},
		"label": {}}
	`)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatal("Error creating request")
		return
	}

	req.Host = "dispatcher.default.127.0.0.1.nip.io"
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}

	for i := 0; i < requests; i++ {
		fmt.Println("Sending request", i)
		if showBody {
			log.Println(req.Body)
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Fatal("Error sending request")
			return
		}
		log.Println(resp.Body)
		resp.Body.Close()
		time.Sleep(30 * time.Millisecond)
	}
}
