package main

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "os"
)

func main() {

    // Open our jsonFile
    jsonFile, err := os.Open("C:/Users/evalencia/OneDrive/01_dataprojects/_tasks/evdotcom_20190923/_site/data/quotes.json")
    // if we os.Open returns an error then handle it
    if err != nil {
        fmt.Println(err)
    }
    fmt.Println("Successfully Opened quotes.json")
    // defer the closing of our jsonFile so that we can parse it later on
    defer jsonFile.Close()

    byteValue, _ := ioutil.ReadAll(jsonFile)

    var result map[string]interface{}
    json.Unmarshal([]byte(byteValue), &result)

    fmt.Println(result["0"])

}