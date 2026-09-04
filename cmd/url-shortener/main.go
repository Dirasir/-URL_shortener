package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"://github.com"
	"://github.com"
)

type Config struct {
	Port        string
	PostgresURL string
	RedisURL    string
}

type Server struct {
	db  *pgx.Conn
	rdb *redis.Client
	ctx context.Context
}

type ShortenRequest struct {
	URL string `json:"url"`
}

type ShortenResponse struct {
	ShortURL string `json:"short_url"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = ":8080"
	}

	pgURL := os.Getenv("POSTGRES_URL")
	if pgURL == "" {
		pgURL = "postgres://postgres:postgres@localhost:5432/shortener?sslmode=disable"
	}

	rdataURL := os.Getenv("REDIS_URL")
	if rdataURL == "" {
		rdataURL = "localhost:6379"
	}

	cfg := Config{
		Port:        port,
		PostgresURL: pgURL,
		RedisURL:    rdataURL,
	}

	ctx := context.Background()

	var db *pgx.Conn
	var err error
	for i := 0; i < 5; i++ {
		db, err = pgx.Connect(ctx, cfg.PostgresURL)
		if err == nil {
			break
		}
		log.Printf("Waiting for Postgres... %v", err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("Postgres connection error: %v", err)
	}
	defer db.Close(ctx)

	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisURL,
	})
	defer rdb.Close()

	srv := &Server{db: db, rdb: rdb, ctx: ctx}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /shorten", srv.handleShorten)
	mux.HandleFunc("GET /{key}", srv.handleRedirect)

	log.Printf("Server starting on %s", cfg.Port)
	if err := http.ListenAndServe(cfg.Port, mux); err != nil {
		log.Fatal(err)
	}
}

func (s *Server) handleShorten(w http.ResponseWriter, r *http.Request) {
	var req ShortenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		http.Error(w, "URL cannot be empty", http.StatusBadRequest)
		return
	}

	key := NewShortKey(6)

	_, err := s.db.Exec(s.ctx, "INSERT INTO urls (short_key, original_url) VALUES ($1, $2)", key, req.URL)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	err = s.rdb.Set(s.ctx, key, req.URL, 24*time.Hour).Err()
	if err != nil {
		log.Printf("Redis write error: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ShortenResponse{ShortURL: "http://localhost:8080/" + key})
}

func (s *Server) handleRedirect(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "Invalid key", http.StatusBadRequest)
		return
	}

	cachedURL, err := s.rdb.Get(s.ctx, key).Result()
	if err == nil {
		http.Redirect(w, r, cachedURL, http.StatusMovedPermanently)
		return
	}

	var originalURL string
	err = s.db.QueryRow(s.ctx, "SELECT original_url FROM urls WHERE short_key = $1", key).Scan(&originalURL)
	if err != nil {
		if err == pgx.ErrNoRows {
			http.Error(w, "URL not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	s.rdb.Set(s.ctx, key, originalURL, 24*time.Hour)

	http.Redirect(w, r, originalURL, http.StatusMovedPermanently)
}
