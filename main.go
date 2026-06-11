package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"wolsender/internal/wol"
)

//go:embed web/*
var embeddedFiles embed.FS

type wakeRequest struct {
	MAC         string `json:"mac"`
	InterfaceID string `json:"interfaceId"`
	Port        int    `json:"port"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func main() {
	host := flag.String("host", "127.0.0.1", "HTTP bind host")
	port := flag.Int("port", 0, "HTTP bind port; 0 selects a free port")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser automatically")
	flag.Parse()

	mux, err := newMux()
	if err != nil {
		log.Fatalf("configure app: %v", err)
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(*host, strconv.Itoa(*port)))
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	url := "http://" + listener.Addr().String() + "/"
	log.Printf("WOL Sender listening on %s", url)

	if !*noBrowser {
		if err := openBrowser(url); err != nil {
			log.Printf("Open %s in your browser. Automatic browser launch failed: %v", url, err)
		}
	}

	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}

func newMux() (*http.ServeMux, error) {
	ui, err := fs.Sub(embeddedFiles, "web")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/interfaces", handleInterfaces)
	mux.HandleFunc("/api/wake", handleWake)
	mux.Handle("/", http.FileServer(http.FS(ui)))
	return mux, nil
}

func handleInterfaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	interfaces, err := wol.ListInterfaces()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"interfaces": interfaces})
}

func handleWake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	defer r.Body.Close()

	var request wakeRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON body"})
		return
	}

	result, err := wol.Wake(request.MAC, request.InterfaceID, request.Port)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

func openBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		command = exec.Command("open", url)
	default:
		command = exec.Command("xdg-open", url)
	}

	if err := command.Start(); err != nil {
		return fmt.Errorf("start browser command: %w", err)
	}
	return nil
}
