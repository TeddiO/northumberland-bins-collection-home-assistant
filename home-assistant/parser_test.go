package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseCSRFToken(t *testing.T) {
	token, err := parseCSRFToken(strings.NewReader(`
		<html>
			<body>
				<input type="hidden" name="_csrf" value="test-token">
			</body>
		</html>
	`))
	if err != nil {
		t.Fatalf("parseCSRFToken returned an error: %v", err)
	}

	if token != "test-token" {
		t.Fatalf("expected test-token, got %q", token)
	}
}

func TestParseCollectionDateRollsIntoNextYear(t *testing.T) {
	now := time.Date(
		2026,
		time.December,
		30,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	date, err := parseCollectionDate("5 January", now)
	if err != nil {
		t.Fatalf("parseCollectionDate returned an error: %v", err)
	}

	expected := time.Date(
		now.Year()+1,
		time.January,
		5,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	if !date.Equal(expected) {
		t.Fatalf("expected %s, got %s", expected, date)
	}
}
