## aicli

`aicli` is AI assistant client library for Go, support ChatGPT and DeepSeek.

<br>

### Example of use

#### ChatGPT

```go
package main

import (
    "context"
    "fmt"
    "github.com/18721889353/sunshine/pkg/aicli/chatgpt"
)

func main() {
    var apiKey = "sk-xxxxxx"
    client, _ := chatgpt.NewClient(apiKey) // you can use set client options, e.g. WithModel(ModelGPT4o)

    // case 1: default
    content, _ := client.Send(context.Background(), "Who are you?)
    fmt.Println(content)

    // case 2: stream
    answer := client.SendStream(context.Background(), "Which model did you use to answer the question?")
    for content := range answer.Content {
        fmt.Printf(content)
    }
    if answer.Err != nil {
        panic(answer.Err)
    }
}
```

<br>

#### DeepSeek

```go
package main

import (
    "context"
    "fmt"
    "github.com/18721889353/sunshine/pkg/aicli/deepseek"
)

func main() {
    var apiKey = "sk-xxxxxx"
    client, _ := deepseek.NewClient(apiKey) // you can use set client options, e.g. WithModel(ModelDeepSeekReasoner)

    // case 1: default
    content, _ := client.Send(context.Background(), "Who are you?)
    fmt.Println(content)

    // case 2: stream
    answer := client.SendStream(context.Background(), "Which model did you use to answer the question?")
    for content := range answer.Content {
        fmt.Printf(content)
    }
    if answer.Err != nil {
        panic(answer.Err)
    }
}
```
