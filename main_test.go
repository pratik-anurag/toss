package main

import "testing"

func TestParseCreateRequestAroundNew(t *testing.T) {
	left, ok, err := parseCreateRequest([]string{"--lang", "flask", "new", "api"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("left-side flags did not parse as create request")
	}

	right, ok, err := parseCreateRequest([]string{"new", "api", "--lang", "flask"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("right-side flags did not parse as create request")
	}

	if left != right {
		t.Fatalf("requests differ: left=%+v right=%+v", left, right)
	}
}

func TestParseCreateRequestTemplateAliasAndNoVenv(t *testing.T) {
	got, ok, err := parseCreateRequest([]string{"new", "demo", "--template", "python", "--no-venv"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("request did not parse as create request")
	}
	if got.name != "demo" || got.template != "python" || !got.noVenv {
		t.Fatalf("unexpected request: %+v", got)
	}
}

func TestParseCreateRequestLeavesCleanFlagsAlone(t *testing.T) {
	_, ok, err := parseCreateRequest([]string{"clean", "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("clean parsed as create request")
	}
}

func TestParseCreateRequestUnknownFlag(t *testing.T) {
	_, _, err := parseCreateRequest([]string{"new", "api", "--wat"})
	if err == nil {
		t.Fatal("unknown flag succeeded, want error")
	}
}
