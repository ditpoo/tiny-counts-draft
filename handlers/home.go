package handlers

import (
	"html/template"
	"log"
	"net/http"
)

type TemplateData struct {
    Message  string
    Counters map[string]int
    Total    int64
    Key      string
}

func HomeHandler(sharedData *SharedData) http.HandlerFunc { 
    return func(w http.ResponseWriter, r *http.Request) {
    tmpl := template.Must(template.ParseFiles("templates/index.html"))

    keys, err := sharedData.BadgerDB.GetAllKeys()
    log.Println(keys)
    if err != nil {
        http.Error(w, "Failed to fetch keys from database", http.StatusInternalServerError)
        return
    }

    counters := make(map[string]int)
    for _, key := range keys {
        counters[key] = 0
    }

    templateData := TemplateData{
        Message:  "Counter App",
        Counters: counters,
        Total:    0,
        Key:      "",
    }

    err = tmpl.Execute(w, templateData)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
    }
}
}
