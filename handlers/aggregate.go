package handlers

import (
	"net/http"
	"html/template"

	"github.com/araddon/dateparse"
)

func AggregateCountsHandler(sharedData *SharedData) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			key = "default"
		}
		startStr := r.URL.Query().Get("start")
		endStr := r.URL.Query().Get("end")

		startTime, err := dateparse.ParseAny(startStr)
		if err != nil {
			http.Error(w, "Invalid start time format", http.StatusBadRequest)
			return
		}
		endTime, err := dateparse.ParseAny(endStr)
		if err != nil {
			http.Error(w, "Invalid end time format", http.StatusBadRequest)
			return
		}

		total, err := sharedData.BadgerDB.AggregateCounts(key, startTime, endTime)
		if err != nil {
			http.Error(w, "Failed to aggregate counts", http.StatusInternalServerError)
			return
		}

		tmpl := template.Must(template.ParseFiles("templates/aggregate.html"))
		data := struct {
			Total int64
			Key   string
		}{
			Total: total,
			Key:   key,
		}
		err = tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
