package main

import (
	"log"
	"net/http"
	"time"

	"github.com/asd1asd00000/sub-merger/internal/db"
	"github.com/asd1asd00000/sub-merger/internal/web"
)

func main() {
	port := "5000"
	mux := http.NewServeMux()

	web.RegisterRoutes(mux)

	// روشن کردن سیستم بکاپ‌گیری خودکار در پس‌زمینه
	db.StartAutoBackup()

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Starting Sub-Merger server on port %s...\n", port)
	
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed to start: %v\n", err)
	}
}
