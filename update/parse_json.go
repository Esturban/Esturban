package main

import (
    "encoding/json"
    "log"
)

func main() {
    dataJson, err := os.Open(".json")
    var arr []string
    _ = json.Unmarshal([]byte(dataJson), &arr)
    log.Printf("Unmarshaled: %v", arr)
}
