# dllama
A simple terminal interface to chat with a remote or local instance Ollama.

## Installation

```bash
go install github.com/ollama/dllama@latest
```

## Usage

After installing the package, you can run the following command to start the chat interface. This assumes your go bin directory is in your PATH.

```bash
dllama -url <url> -model <model>
```

#### For Example

```bash
dllama -url http://localhost:11434 -model llama3.2
```

## Configuration

It is recommended that you create a configuration file by running the following command:

```bash
dllama --config
```

This will create a configuration file with default values for your Ollama URL and your default model. After your configuration file you no longer need to specify the URL and model in the command line. Doing so will override the values in the configuration file.

```bash

## Commands

- `-config`: Create a configuration file.
- `-help`: Display help information.
- `-list-models`: List available models.
- `-model <model>`: Sets the model to use when chatting with Ollama.
- `-url <url>`: Set the Ollama URL.