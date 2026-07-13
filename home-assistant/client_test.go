package main

import (
	"context"
	"testing"
	"time"
)

const (
	liveCollectionUPRN   = "010001018389"
	liveNoCollectionUPRN = "100110769935"
)

func TestFetchCollectionsLive(t *testing.T) {
	client, err := newClient(defaultBaseURL, false)
	if err != nil {
		t.Fatalf("newClient returned an error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	result, err := client.fetchCollections(ctx, liveCollectionUPRN)
	if err != nil {
		t.Fatalf("fetchCollections returned an error: %v", err)
	}

	if result.UPRN != liveCollectionUPRN {
		t.Fatalf("expected UPRN %q, got %q", liveCollectionUPRN, result.UPRN)
	}

	if result.Address == "" {
		t.Fatal("expected an address")
	}

	if !result.Available {
		t.Fatalf(
			"expected collections to be available, got message %q",
			result.Message,
		)
	}

	if len(result.Collections) == 0 {
		t.Fatal("expected at least one collection")
	}

	for index, collection := range result.Collections {
		if _, err := time.Parse("2006-01-02", collection.Date); err != nil {
			t.Errorf(
				"collection %d has invalid date %q: %v",
				index,
				collection.Date,
				err,
			)
		}

		if collection.Day == "" {
			t.Errorf("collection %d has an empty day", index)
		}

		if collection.Label == "" {
			t.Errorf("collection %d has an empty label", index)
		}

		switch collection.Type {
		case "recycling", "general":
		default:
			t.Errorf(
				"collection %d has unexpected type %q",
				index,
				collection.Type,
			)
		}
	}
}

func TestFetchCollectionsWithNoCollectionDaysLive(t *testing.T) {
	client, err := newClient(defaultBaseURL, false)
	if err != nil {
		t.Fatalf("newClient returned an error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	result, err := client.fetchCollections(ctx, liveNoCollectionUPRN)
	if err != nil {
		t.Fatalf("fetchCollections returned an error: %v", err)
	}

	if result.UPRN != liveNoCollectionUPRN {
		t.Fatalf(
			"expected UPRN %q, got %q",
			liveNoCollectionUPRN,
			result.UPRN,
		)
	}

	if result.Address == "" {
		t.Fatal("expected an address")
	}

	if result.Available {
		t.Fatal("expected collections to be unavailable")
	}

	if result.Message != "No upcoming bin collection days available for this address." {
		t.Fatalf("unexpected message: %q", result.Message)
	}

	if result.Collections == nil {
		t.Fatal("expected an empty collection slice, got nil")
	}

	if len(result.Collections) != 0 {
		t.Fatalf(
			"expected no collections, got %d",
			len(result.Collections),
		)
	}
}
