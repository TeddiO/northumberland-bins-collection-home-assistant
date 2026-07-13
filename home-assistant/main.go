package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	htmlstd "html"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://bincollection.northumberland.gov.uk"

	statusOK                 = "ok"
	statusNoCollections      = "no_collections"
	statusValidationRequired = "validation_required"
	statusError              = "error"
)

var (
	ErrValidationRequired = errors.New("NCC browser validation required")

	inputPattern     = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	tablePattern     = regexp.MustCompile(`(?is)<table\b[^>]*>(.*?)</table>`)
	rowPattern       = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	cellPattern      = regexp.MustCompile(`(?is)<(?:th|td)\b[^>]*>(.*?)</(?:th|td)>`)
	addressPattern   = regexp.MustCompile(`(?is)<p\b[^>]*class=["'][^"']*\bncc-body\b[^"']*["'][^>]*>(.*?)</p>`)
	titlePattern     = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
	tagPattern       = regexp.MustCompile(`(?is)<[^>]+>`)
	spacePattern     = regexp.MustCompile(`\s+`)
	attributePattern = regexp.MustCompile(`(?is)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

type AppOptions struct {
	UPRN string `json:"uprn"`
}

type Collection struct {
	Date  string `json:"date"`
	Day   string `json:"day"`
	Type  string `json:"type"`
	Label string `json:"label"`
}

type Result struct {
	UPRN        string       `json:"uprn"`
	Address     string       `json:"address,omitempty"`
	Status      string       `json:"status"`
	Available   bool         `json:"available"`
	Message     string       `json:"message,omitempty"`
	Warning     string       `json:"warning,omitempty"`
	FetchedAt   time.Time    `json:"fetched_at"`
	Collections []Collection `json:"collections"`
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	now        func() time.Time
	debug      bool
}

func main() {
	debugDefault, err := envBool("DEBUG")
	if err != nil {
		log.Fatal(err)
	}

	uprn := flag.String("uprn", os.Getenv("UPRN"), "NCC address identifier/UPRN")
	baseURL := flag.String(
		"base-url",
		envOrDefault("NCC_BASE_URL", defaultBaseURL),
		"NCC bin collection base URL",
	)
	debug := flag.Bool("debug", debugDefault, "print NCC HTTP responses for debugging")
	listenAddress := flag.String(
		"listen-address",
		envOrDefault("LISTEN_ADDRESS", ":8080"),
		"HTTP listen address",
	)

	flag.Parse()

	trimmedUPRN := strings.TrimSpace(*uprn)

	if trimmedUPRN == "" {
		trimmedUPRN, err = loadHomeAssistantUPRN()
		if err != nil {
			log.Fatal(err)
		}
	}

	if trimmedUPRN == "" {
		log.Fatal("UPRN is required; set UPRN, pass -uprn, or configure the Home Assistant app")
	}

	client, err := newClient(*baseURL, *debug)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	handler := http.NewServeMux()

	handler.HandleFunc("GET /collections", func(writer http.ResponseWriter, request *http.Request) {
		result, fetchErr := client.fetchCollections(request.Context(), trimmedUPRN)
		if fetchErr != nil {
			result = resultFromError(trimmedUPRN, client.now(), fetchErr)
		}

		writer.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(writer).Encode(result); err != nil {
			log.Printf("encode response: %v", err)
		}
	})

	handler.HandleFunc("GET /health", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)

		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{
		Addr:              *listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("listening on %s", *listenAddress)

	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func loadHomeAssistantUPRN() (string, error) {
	body, err := os.ReadFile("/data/options.json")
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("read Home Assistant options: %w", err)
	}

	var options AppOptions

	if err := json.Unmarshal(body, &options); err != nil {
		return "", fmt.Errorf("decode Home Assistant options: %w", err)
	}

	return strings.TrimSpace(options.UPRN), nil
}

func newClient(baseURL string, debug bool) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
		now:   time.Now,
		debug: debug,
	}, nil
}

func (client *Client) fetchCollections(ctx context.Context, uprn string) (Result, error) {
	uprn = strings.TrimSpace(uprn)
	if uprn == "" {
		return Result{}, fmt.Errorf("UPRN must not be empty")
	}

	csrfToken, err := client.fetchCSRFToken(ctx)
	if err != nil {
		return Result{}, err
	}

	body, err := client.submitAddress(ctx, csrfToken, uprn)
	if err != nil {
		return Result{}, err
	}
	defer body.Close()

	address, collections, err := parseCollections(body, client.now())
	if err != nil {
		return Result{}, fmt.Errorf("parse collections: %w", err)
	}

	result := Result{
		UPRN:        uprn,
		Address:     address,
		Status:      statusOK,
		Available:   len(collections) > 0,
		FetchedAt:   client.now().UTC(),
		Collections: collections,
	}

	if !result.Available {
		result.Status = statusNoCollections
		result.Message = "No upcoming bin collection days available for this address."
	}

	return result, nil
}

func (client *Client) fetchCSRFToken(ctx context.Context) (string, error) {
	requestURL := fmt.Sprintf("%s/address-select", client.baseURL)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", fmt.Errorf("create CSRF request: %w", err)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch CSRF token: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read CSRF response: %w", err)
	}

	if client.debug {
		debugHTTPResponse("CSRF GET", response, body)
	}

	if isValidationPage(body) {
		return "", ErrValidationRequired
	}

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf(
			"fetch CSRF token returned %s, final URL %s, body preview %q",
			response.Status,
			response.Request.URL,
			bodyPreview(body),
		)
	}

	token, err := parseCSRFToken(strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf(
			"parse CSRF token: %w; final URL: %s; page title: %q; body preview: %q",
			err,
			response.Request.URL,
			findHTMLTitle(string(body)),
			bodyPreview(body),
		)
	}

	return token, nil
}

func (client *Client) submitAddress(ctx context.Context, csrfToken string, uprn string) (io.ReadCloser, error) {
	form := url.Values{
		"_csrf":   {csrfToken},
		"address": {uprn},
	}

	requestURL := fmt.Sprintf("%s/address-select", client.baseURL)

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		requestURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("create address request: %w", err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("submit address: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read address response: %w", err)
	}

	if client.debug {
		debugHTTPResponse("ADDRESS POST", response, body)
	}

	if isValidationPage(body) {
		return nil, ErrValidationRequired
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"submit address returned %s, final URL %s, body preview %q",
			response.Status,
			response.Request.URL,
			bodyPreview(body),
		)
	}

	return io.NopCloser(strings.NewReader(string(body))), nil
}

func isValidationPage(body []byte) bool {
	lowerBody := strings.ToLower(string(body))

	return strings.Contains(lowerBody, "<title>validation request</title>") ||
		strings.Contains(lowerBody, "user validation required to continue") ||
		strings.Contains(lowerBody, `action="/captcha_resp"`) ||
		strings.Contains(lowerBody, `src = "/captcha.gif"`) ||
		strings.Contains(lowerBody, `src="/captcha.gif"`)
}

func resultFromError(uprn string, now time.Time, err error) Result {
	result := Result{
		UPRN:        uprn,
		Status:      statusError,
		Available:   false,
		Warning:     err.Error(),
		FetchedAt:   now.UTC(),
		Collections: []Collection{},
	}

	if errors.Is(err, ErrValidationRequired) {
		result.Status = statusValidationRequired
		result.Warning = "Northumberland County Council requires browser validation. Collection data could not be refreshed."
	}

	return result
}

func encodeResult(result Result) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	return encoder.Encode(result)
}

func parseCSRFToken(reader io.Reader) (string, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read HTML: %w", err)
	}

	for _, input := range inputPattern.FindAllString(string(body), -1) {
		attributes := parseAttributes(input)

		if attributes["name"] != "_csrf" {
			continue
		}

		token := strings.TrimSpace(attributes["value"])
		if token == "" {
			return "", fmt.Errorf("CSRF token is empty")
		}

		return token, nil
	}

	return "", fmt.Errorf("CSRF input not found")
}

func parseCollections(reader io.Reader, now time.Time) (string, []Collection, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", nil, fmt.Errorf("read HTML: %w", err)
	}

	htmlBody := string(body)
	address := findAddress(htmlBody)

	if hasNoCollectionsMessage(htmlBody) {
		return address, []Collection{}, nil
	}

	table, err := findCollectionTable(htmlBody)
	if err != nil {
		return "", nil, err
	}

	collections := make([]Collection, 0)

	for _, rowMatch := range rowPattern.FindAllStringSubmatch(table, -1) {
		cells := extractCells(rowMatch[1])

		if len(cells) != 3 || strings.EqualFold(cells[0], "Date") {
			continue
		}

		date, err := parseCollectionDate(cells[0], now)
		if err != nil {
			return "", nil, fmt.Errorf("parse collection date %q: %w", cells[0], err)
		}

		collections = append(collections, Collection{
			Date:  date.Format("2006-01-02"),
			Day:   cells[1],
			Type:  normaliseType(cells[2]),
			Label: cells[2],
		})
	}

	if len(collections) == 0 {
		return "", nil, fmt.Errorf("collection table contained no collection rows")
	}

	return address, collections, nil
}

func hasNoCollectionsMessage(body string) bool {
	lowerBody := strings.ToLower(htmlstd.UnescapeString(body))

	return strings.Contains(lowerBody, "no upcoming bin collection days available for your address")
}

func findCollectionTable(body string) (string, error) {
	for _, tableMatch := range tablePattern.FindAllStringSubmatch(body, -1) {
		rows := rowPattern.FindAllStringSubmatch(tableMatch[1], -1)
		if len(rows) == 0 {
			continue
		}

		headers := extractCells(rows[0][1])
		if len(headers) != 3 {
			continue
		}

		if strings.EqualFold(headers[0], "Date") &&
			strings.EqualFold(headers[1], "Day") &&
			strings.EqualFold(headers[2], "Type") {
			return tableMatch[1], nil
		}
	}

	return "", fmt.Errorf("collection table not found")
}

func extractCells(row string) []string {
	matches := cellPattern.FindAllStringSubmatch(row, -1)
	cells := make([]string, 0, len(matches))

	for _, match := range matches {
		cells = append(cells, cleanText(match[1]))
	}

	return cells
}

func findAddress(body string) string {
	for _, match := range addressPattern.FindAllStringSubmatch(body, -1) {
		address := cleanText(match[1])

		if strings.Contains(address, ",") {
			return address
		}
	}

	return ""
}

func findHTMLTitle(body string) string {
	match := titlePattern.FindStringSubmatch(body)
	if len(match) != 2 {
		return ""
	}

	return cleanText(match[1])
}

func parseCollectionDate(value string, now time.Time) (time.Time, error) {
	location := now.Location()

	dateValue := fmt.Sprintf("%s %d", value, now.Year())

	candidate, err := time.ParseInLocation("2 January 2006", dateValue, location)
	if err != nil {
		return time.Time{}, err
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	if candidate.Before(today) {
		candidate = candidate.AddDate(1, 0, 0)
	}

	return candidate, nil
}

func normaliseType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "recycling":
		return "recycling"

	case "general", "general waste":
		return "general"

	case "garden", "garden waste":
		return "garden"

	default:
		normalised := strings.ReplaceAll(strings.TrimSpace(value), " ", "_")
		return strings.ToLower(normalised)
	}
}

func parseAttributes(tag string) map[string]string {
	attributes := make(map[string]string)

	for _, match := range attributePattern.FindAllStringSubmatch(tag, -1) {
		value := match[2]

		if value == "" {
			value = match[3]
		}

		if value == "" {
			value = match[4]
		}

		attributes[strings.ToLower(match[1])] = htmlstd.UnescapeString(value)
	}

	return attributes
}

func cleanText(value string) string {
	value = tagPattern.ReplaceAllString(value, " ")
	value = htmlstd.UnescapeString(value)

	return strings.TrimSpace(spacePattern.ReplaceAllString(value, " "))
}

func bodyPreview(body []byte) string {
	text := cleanText(string(body))

	const limit = 300

	if len(text) <= limit {
		return text
	}

	return fmt.Sprintf("%s...", text[:limit])
}

func debugHTTPResponse(label string, response *http.Response, body []byte) {
	fmt.Fprintf(os.Stderr, "\n========== DEBUG: %s ==========\n", label)
	fmt.Fprintf(os.Stderr, "Status: %s\n", response.Status)
	fmt.Fprintf(os.Stderr, "Final URL: %s\n", response.Request.URL)
	fmt.Fprintln(os.Stderr, "Headers:")

	for name, values := range response.Header {
		for _, value := range values {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", name, value)
		}
	}

	fmt.Fprintln(os.Stderr, "\nBody:")
	fmt.Fprintln(os.Stderr, string(body))
	fmt.Fprintln(os.Stderr, "========== END DEBUG ==========")
}

func envOrDefault(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}

	return value
}

func envBool(name string) (bool, error) {
	value := strings.ToLower(os.Getenv(name))

	switch value {
	case "":
		return false, nil

	case "true":
		return true, nil

	case "false":
		return false, nil

	default:
		return false, fmt.Errorf("%s must be either true or false", name)
	}
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s -uprn <UPRN>\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}
