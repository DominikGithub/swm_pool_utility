package main

import (
	"fmt"
	"log"
	"net/http"
)

func startWebServer() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<h1>SWM Pool Utility Monitor</h1>")
		fmt.Fprintf(w, "<p>Web server is running!</p>")
		fmt.Fprintf(w, "<a href='/api/pools'>View API Pools</a><br>")
		fmt.Fprintf(w, "<a href='/api/data'>View API Data</a>")
	})

	http.HandleFunc("/api/pools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `["Pool1", "Pool2", "Pool3"]`)
	})

	http.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"labels": ["2024-01-01"], "datasets": [{"label": "Test Pool", "data": [50]}]}`)
	})

	fmt.Println("Starting web server on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}