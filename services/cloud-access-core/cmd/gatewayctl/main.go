package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/gateway"
)

const (
	defaultRoutesEndpoint = "http://127.0.0.1:8080/_admin/gateway/routes"
	defaultCertEndpoint   = "http://127.0.0.1:8080/_admin/gateway/certs"
	defaultTokenEnv       = "GATEWAYCTL_TOKEN"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	cmd := os.Args[1]
	switch cmd {
	case "routes":
		runRoutes(os.Args[2:])
	case "certs":
		runCerts(os.Args[2:])
	default:
		usage()
	}
}

func runRoutes(args []string) {
	if len(args) == 0 {
		usageRoutes()
		return
	}
	switch args[0] {
	case "push":
		pushRoutes(args[1:])
	default:
		usageRoutes()
	}
}

func pushRoutes(args []string) {
	flags := flag.NewFlagSet("routes push", flag.ExitOnError)
	file := flags.String("file", "configs/gateway.routes.yaml", "path to routes file (yaml or json)")
	addr := flags.String("addr", defaultRoutesEndpoint, "gateway admin routes endpoint")
	token := flags.String("token", os.Getenv(defaultTokenEnv), "admin token (or set GATEWAYCTL_TOKEN)")
	_ = flags.Parse(args)

	body, err := os.ReadFile(*file)
	checkErr(err, "read routes file")

	set, err := gateway.ParseRouteSet(body)
	checkErr(err, "parse routes file")

	payload, err := json.Marshal(set)
	checkErr(err, "encode payload")

	resp, err := doRequest(*addr, *token, payload)
	checkErr(err, "push routes")
	defer resp.Body.Close()

	handleResponse(resp, "routes updated")
}

func runCerts(args []string) {
	if len(args) == 0 {
		usageCerts()
		return
	}
	switch args[0] {
	case "rotate":
		rotateCerts(args[1:])
	default:
		usageCerts()
	}
}

func rotateCerts(args []string) {
	flags := flag.NewFlagSet("certs rotate", flag.ExitOnError)
	certPath := flags.String("cert", "", "path to PEM certificate")
	keyPath := flags.String("key", "", "path to PEM private key")
	addr := flags.String("addr", defaultCertEndpoint, "gateway admin cert endpoint")
	token := flags.String("token", os.Getenv(defaultTokenEnv), "admin token (or set GATEWAYCTL_TOKEN)")
	_ = flags.Parse(args)

	if *certPath == "" || *keyPath == "" {
		fmt.Fprintln(os.Stderr, "cert and key paths are required")
		os.Exit(1)
	}

	cert, err := os.ReadFile(*certPath)
	checkErr(err, "read certificate file")
	key, err := os.ReadFile(*keyPath)
	checkErr(err, "read private key file")

	payload, err := json.Marshal(map[string]string{
		"certificate": string(cert),
		"private_key": string(key),
	})
	checkErr(err, "encode payload")

	resp, err := doRequest(*addr, *token, payload)
	checkErr(err, "rotate certs")
	defer resp.Body.Close()
	handleResponse(resp, "certificate stored")
}

func doRequest(addr, token string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, addr, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Admin-Token", token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	return client.Do(req)
}

func handleResponse(resp *http.Response, successMessage string) {
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Printf("%s (status=%d)\n", successMessage, resp.StatusCode)
		fmt.Println(string(respBody))
		return
	}
	fmt.Fprintf(os.Stderr, "request failed: status=%d body=%s\n", resp.StatusCode, string(respBody))
	os.Exit(1)
}

func usage() {
	fmt.Println("Usage: gatewayctl <routes|certs> <subcommand> [flags]")
	fmt.Println()
	usageRoutes()
	usageCerts()
}

func usageRoutes() {
	fmt.Println("Routes commands:")
	fmt.Println("  gatewayctl routes push --file configs/gateway.routes.yaml [--addr URL] [--token TOKEN]")
}

func usageCerts() {
	fmt.Println("Cert commands:")
	fmt.Println("  gatewayctl certs rotate --cert tls.crt --key tls.key [--addr URL] [--token TOKEN]")
}

func checkErr(err error, msg string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
		os.Exit(1)
	}
}
