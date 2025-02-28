package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
)

// const ollamaUrl = "http://bulbasaur.bearded-piano.ts.net:11434"

const configFileName = "config.json"
const defaultOllamaUrl = "http://localhost:11434"
const logo = ` 
 ┓┓ ┓       
┏┫┃ ┃┏┓┏┳┓┏┓
┗┻┗┛┗┗┻┛┗┗┗┻

v0.0.1
`

// A program that lets your talk to ollama from the command line and formats the responses nicely and streams the responses
// back to the user in real time.
func main() {
	fmt.Printf("%s\n", logo)

	var ollamaUrl string
	var model string
	var config bool
	var listModels bool

	flag.StringVar(&ollamaUrl, "url", "", "The url of the ollama server. Most likely this is something like http://localhost:11434")
	flag.StringVar(&model, "model", "", "The model to use for the chat")
	flag.BoolVar(&config, "config", false, "Whether to configure the chat")
	flag.BoolVar(&listModels, "list-models", false, "List all available models")

	// Parse the flags
	flag.Parse()

	// Setup configuration file to store default values for the url and the model
	if config {
		var config DLLamaConfig

		// // Prompt the user for their url and default model
		var url string
		var defaultModel string

		fmt.Printf("Enter the url of the ollama server: (%s)\n\n", defaultOllamaUrl)
		fmt.Scanln(&url)

		if url == "" {
			url = defaultOllamaUrl
		}

		fmt.Printf("\nCalling %s to get available models...\n", url)

		defaultModel = promptForModelSelection(url)

		config.Url = url
		config.DefaultModel = defaultModel

		err := saveToConfigFile(&config)

		if err != nil {
			fmt.Printf("Error saving config: %v\n", err)
			return
		}

		fmt.Printf("\nConfiguration saved.\n\n")
		return
	}

	// Make sure the url and the model are not empty
	if ollamaUrl == "" || model == "" {

		// if they are not provided, try to get them from the config file
		config, err := readFromConfigFile()

		if err != nil {
			if err == ErrConfigFileNotFound {
				fmt.Printf("Necessary values not provided. Please provide the url and model using the command line flags.\n\ndllama -url http://localhost:11434/ -model llama3.2\n\nAlternatively, default values can be set using the -config option.\n\nFor more information use the -help flag.\n\n")
				return
			}

			fmt.Printf("Error reading config file: %v\n", err)
			return
		}

		if config != nil {
			if ollamaUrl == "" {
				ollamaUrl = config.Url
			}

			if model == "" {
				model = config.DefaultModel
			}
		}
	}

	if listModels {
		models := fetchAvailableModels(ollamaUrl)

		if models == nil {
			fmt.Println("Error getting models")
			return
		}

		fmt.Printf("\nAvailable models:\n\n")

		for _, m := range models {
			fmt.Println(m)
		}

		fmt.Println()

		return
	}

	reader := bufio.NewReader(os.Stdin)

	var conversationHistory []ChatMessage

	for {
		fmt.Printf("Enter your prompt (or type 'exit' to quit): \n\n")

		prompt, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("\nError reading input: %v", err)
			return
		}

		prompt = prompt[:len(prompt)-1]

		if prompt == "exit" {
			break
		}

		conversationHistory = append(conversationHistory, ChatMessage{
			Role:    "user",
			Content: prompt,
		})

		fmt.Println()

		httpClient := http.Client{}

		// Create chat request with history
		chatRequest := ChatRequest{
			Model:    model,
			Prompt:   prompt,
			Messages: conversationHistory,
		}

		reqBody, err := json.Marshal(chatRequest)

		if err != nil {
			fmt.Printf("Error marshalling request: %v\n", err)
			return
		}

		resp, err := httpClient.Post(ollamaUrl+"/api/chat", "application/json", bytes.NewBuffer(reqBody))

		if err != nil {
			fmt.Printf("Error sending request: %v\n", err)
			return
		}

		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)

		var assistantResponse string

		for scanner.Scan() {
			var streamResp StreamResponse

			err = json.Unmarshal(scanner.Bytes(), &streamResp)

			if err != nil {
				fmt.Printf("Error decoding stream: %v\n", err)
				continue
			}

			assistantResponse += streamResp.Message.Content
			fmt.Print(streamResp.Message.Content)

			if streamResp.Done {
				fmt.Println()
				break
			}
		}

		if err := scanner.Err(); err != nil {
			fmt.Printf("Error reading stream: %v\n", err)
		}

		conversationHistory = append(conversationHistory, ChatMessage{
			Role:    "assistant",
			Content: assistantResponse,
		})

		fmt.Println()
	}

	fmt.Println()
}

func fetchAvailableModels(ollamaUrl string) []string {
	httpClient := http.Client{}

	resp, err := httpClient.Get(ollamaUrl + "/api/tags")

	if err != nil {
		fmt.Printf("Error getting models: %v\n", err)
		return nil
	}

	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)

	var response ListModelsResponse

	for scanner.Scan() {
		err = json.Unmarshal(scanner.Bytes(), &response)

		if err != nil {
			fmt.Printf("Error decoding response: %v\n", err)
			return nil
		}
	}

	var models []string

	for _, m := range response.Models {
		models = append(models, m.Name)
	}

	return models
}

func saveToConfigFile(config *DLLamaConfig) error {
	configPath, err := getUserConfigPath()

	if err != nil {
		return fmt.Errorf("error getting user config path: %w", err)
	}

	configDir := filepath.Dir(configPath)

	err = os.MkdirAll(configDir, 0755)

	if err != nil {
		return fmt.Errorf("error creating config directory: %w", err)
	}

	configFile, err := os.Create(configPath)

	if err != nil {
		return fmt.Errorf("error creating config file: %w", err)
	}

	defer configFile.Close()

	err = json.NewEncoder(configFile).Encode(config)

	if err != nil {
		return fmt.Errorf("error encoding config file: %w", err)
	}

	return nil
}

func readFromConfigFile() (*DLLamaConfig, error) {
	configPath, err := getUserConfigPath()

	if err != nil {
		return nil, fmt.Errorf("error getting user config path: %w", err)
	}

	configFile, err := os.Open(configPath)

	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrConfigFileNotFound
		}

		return nil, fmt.Errorf("error opening config file: %w", err)
	}

	defer configFile.Close()

	var config DLLamaConfig

	err = json.NewDecoder(configFile).Decode(&config)

	if err != nil {
		return nil, fmt.Errorf("error decoding config file: %w", err)
	}

	return &config, nil
}

func promptForModelSelection(url string) string {
	models := fetchAvailableModels(url)

	if models == nil {
		fmt.Println("Error getting models")
		return ""
	}

	fmt.Printf("\nAvailable models:\n\n")

	for i, m := range models {
		fmt.Printf("%d. %s\n", i+1, m)
	}

	fmt.Println()

	var selection int

	for {
		fmt.Printf("Enter the number of the model you would like to use: \n\n")

		_, err := fmt.Scanln(&selection)

		if err != nil {
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		if selection < 1 || selection > len(models) {
			fmt.Println("Invalid selection. Please try again.")
			continue
		}

		break
	}

	return models[selection-1]
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Prompt   string        `json:"prompt"`
	Messages []ChatMessage `json:"messages"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type StreamResponse struct {
	Model     string      `json:"model"`
	CreatedAt string      `json:"created_at"`
	Message   ChatMessage `json:"message"`
	Done      bool        `json:"done"`
}

type DLLamaConfig struct {
	Url          string `json:"url"`
	DefaultModel string `json:"default_model"`
}

type ListModelsResponse struct {
	Models []Model `json:"models"`
}

func getUserConfigPath() (string, error) {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA environment variable is not set")
		}
		return filepath.Join(appData, "dllama", configFileName), nil
	} else {
		usr, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("error getting current user: %w", err)
		}
		return filepath.Join(usr.HomeDir, ".config", "dllama", configFileName), nil
	}
}

type Model struct {
	Name    string       `json:"name"`
	Details ModelDetails `json:"details"`
}

type ModelDetails struct {
	Format            string `json:"format"`
	Family            string `json:"family"`
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}

var ErrConfigFileNotFound = fmt.Errorf("config file does not exist")
