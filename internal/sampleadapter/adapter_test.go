package sampleadapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestSampleAdapterReadOnlyProtocol(t *testing.T) {
	t.Parallel()
	client, host := net.Pipe()
	adapter, err := newServer(host)
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- adapter.serve(host) }()
	defer func() {
		_ = client.Close()
		if err := <-serveDone; err != nil {
			t.Errorf("serve adapter: %v", err)
		}
	}()
	reader := bufio.NewReader(client)
	call := func(id, method string, params map[string]any) map[string]any {
		t.Helper()
		request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
		frame, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Write(append(frame, '\n')); err != nil {
			t.Fatal(err)
		}
		responseLine, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var response map[string]any
		if err := json.Unmarshal(responseLine, &response); err != nil {
			t.Fatalf("decode response %q: %v", responseLine, err)
		}
		return response
	}
	params := func(fields map[string]any) map[string]any {
		fields["deadline"] = time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
		fields["budget"] = map[string]any{"maxItems": 10, "maxBytes": maxBytes}
		return fields
	}
	result := func(response map[string]any) map[string]any {
		t.Helper()
		value, ok := response["result"].(map[string]any)
		if !ok {
			t.Fatalf("response has no result: %#v", response)
		}
		return value
	}

	initialized := call("1", "initialize", map[string]any{"protocolMajors": []int{1}, "hostFeatures": []string{}})
	manifest := result(initialized)["manifest"].(map[string]any)
	if manifest["id"] != "worklease.sample.static" || manifest["resourcePolicy"] != "generic" {
		t.Fatalf("manifest = %#v", manifest)
	}
	resolved := call("2", "resolve", params(map[string]any{"sourceId": "sample", "config": map[string]any{}}))
	if result(resolved)["source"].(map[string]any)["id"] != "sample" {
		t.Fatalf("resolved source = %#v", result(resolved)["source"])
	}

	firstPageParams := params(map[string]any{"sourceId": "sample", "query": map[string]any{"states": []string{}, "text": ""}, "cursor": nil, "fields": []string{}})
	firstPageParams["budget"] = map[string]any{"maxItems": 1, "maxBytes": maxBytes}
	firstPage := result(call("3", "list", firstPageParams))
	firstItems := firstPage["items"].([]any)
	if len(firstItems) != 1 || firstItems[0].(map[string]any)["ref"].(map[string]any)["itemId"] != "sample-1" || firstPage["nextCursor"] == nil || firstPage["context"].(map[string]any)["coverage"].(map[string]any)["state"] != "partial" {
		t.Fatalf("first page = %#v", firstPage)
	}
	secondPageParams := params(map[string]any{"sourceId": "sample", "query": map[string]any{"states": []string{}, "text": ""}, "cursor": firstPage["nextCursor"], "fields": []string{}})
	secondPageParams["budget"] = map[string]any{"maxItems": 1, "maxBytes": maxBytes}
	secondPage := result(call("4", "list", secondPageParams))
	if len(secondPage["items"].([]any)) != 1 || secondPage["nextCursor"] == nil || secondPage["context"].(map[string]any)["coverage"].(map[string]any)["state"] != "partial" {
		t.Fatalf("second page = %#v", secondPage)
	}
	thirdPageParams := params(map[string]any{"sourceId": "sample", "query": map[string]any{"states": []string{}, "text": ""}, "cursor": secondPage["nextCursor"], "fields": []string{}})
	thirdPageParams["budget"] = map[string]any{"maxItems": 1, "maxBytes": maxBytes}
	thirdPage := result(call("5", "list", thirdPageParams))
	if len(thirdPage["items"].([]any)) != 1 || thirdPage["nextCursor"] != nil || thirdPage["context"].(map[string]any)["coverage"].(map[string]any)["state"] != "complete" {
		t.Fatalf("third page = %#v", thirdPage)
	}
	mismatchedListCursor := call("10", "list", params(map[string]any{
		"sourceId": "sample", "query": map[string]any{"states": []string{"complete"}, "text": ""},
		"cursor": firstPage["nextCursor"], "fields": []string{},
	}))
	listError, ok := mismatchedListCursor["error"].(map[string]any)
	listErrorData, dataOK := listError["data"].(map[string]any)
	if !ok || !dataOK || listErrorData["diagnostic"] != "invalid-params" {
		t.Fatalf("list cursor was accepted with different filters: %#v", mismatchedListCursor)
	}

	refs := []any{
		map[string]string{"sourceId": "sample", "itemId": "sample-1"},
		map[string]string{"sourceId": "sample", "itemId": "sample-private"},
		map[string]string{"sourceId": "sample", "itemId": "not-present"},
	}
	readItems := result(call("6", "readItems", params(map[string]any{"sourceId": "sample", "refs": refs, "fields": []string{}})))["outcomes"].([]any)
	if len(readItems) != 3 || readItems[0].(map[string]any)["status"] != "found" || readItems[1].(map[string]any)["status"] != "inaccessible" || readItems[2].(map[string]any)["status"] != "missing" {
		t.Fatalf("read outcomes = %#v", readItems)
	}

	firstDepsParams := params(map[string]any{"ref": map[string]string{"sourceId": "sample", "itemId": "sample-1"}, "cursor": nil})
	firstDepsParams["budget"] = map[string]any{"maxItems": 1, "maxBytes": maxBytes}
	firstDeps := result(call("7", "readDependencies", firstDepsParams))
	if len(firstDeps["edges"].([]any)) != 1 || firstDeps["completeness"] != "partial" || firstDeps["nextCursor"] == nil {
		t.Fatalf("first dependency page = %#v", firstDeps)
	}
	mismatchedDependencyCursor := call("11", "readDependencies", params(map[string]any{
		"ref": map[string]string{"sourceId": "sample", "itemId": "sample-2"}, "cursor": firstDeps["nextCursor"],
	}))
	dependencyError, ok := mismatchedDependencyCursor["error"].(map[string]any)
	dependencyErrorData, dataOK := dependencyError["data"].(map[string]any)
	if !ok || !dataOK || dependencyErrorData["diagnostic"] != "invalid-params" {
		t.Fatalf("dependency cursor was accepted for a different ref: %#v", mismatchedDependencyCursor)
	}
	policy := result(call("8", "resourcePolicy", params(map[string]any{
		"ref": map[string]string{"sourceId": "sample", "itemId": "sample-1"}, "workKey": "example-work",
	})))
	if policy["policy"] != "generic" || policy["source"] != "sample" || policy["item"] != "sample-1" || policy["scope"] != "item" {
		t.Fatalf("resource policy = %#v", policy)
	}

	mutationParams := params(map[string]any{
		"ref": map[string]string{"sourceId": "sample", "itemId": "sample-1"}, "operationId": "operation-1",
		"patch": map[string]any{"state": "complete"}, "authority": map[string]string{"authorizationRef": "caller", "scope": "sample-1"},
	})
	mutation := call("9", "writeState", mutationParams)
	failure := mutation["error"].(map[string]any)
	if failure["code"] != float64(-32001) || failure["data"].(map[string]any)["diagnostic"] != "unsupported-capability" {
		t.Fatalf("mutation response = %#v", mutation)
	}
	if strings.Contains(string(mustMarshal(mutation)), "sample-fixture") {
		t.Fatalf("unsupported mutation exposed fixture data: %#v", mutation)
	}
}

func TestSampleAdapterLaunchedByExternalHost(t *testing.T) {
	_, paths := testkit.Home(t)
	env := func(name string) string {
		if value, ok := paths[name]; ok {
			return value
		}
		return os.Getenv(name)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "sample-adapter")
	command := fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^TestSampleAdapterProcessHelper$' -- sample-adapter-process\n", shellQuote(testBinary))
	if err := os.WriteFile(script, []byte(command), 0o700); err != nil {
		t.Fatal(err)
	}
	canonicalScript, err := filepath.EvalSymlinks(script)
	if err != nil {
		t.Fatal(err)
	}
	source := config.QueueSource{
		ID: "sample-host-test", Adapter: "external", Executable: canonicalScript,
		ExpectedAdapterID: "worklease.sample.static", ExpectedVersion: "1.0.0",
		Config: map[string]any{},
	}
	if err := config.ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatalf("approve sample adapter shim: %v", err)
	}
	registry := queue.NewRegistry()
	cleanup, err := queue.RegisterExternalSources(registry, []config.QueueSource{source}, env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	adapter, ok := registry.Get(queue.ExternalSourceAdapterKey(source.ID))
	if !ok {
		t.Fatal("external host did not register sample adapter")
	}
	resolved, err := adapter.Resolve(context.Background(), map[string]string{"id": source.ID})
	if err != nil || resolved.ID != source.ID || resolved.Locator != "memory://sample-fixture" {
		t.Fatalf("host resolution = %+v, error=%v", resolved, err)
	}
	page, err := adapter.List(context.Background(), resolved, queue.Query{Budget: 1, Fields: []string{"title"}}, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].Ref.ItemID != "sample-1" || page.NextCursor == "" || page.Coverage.State != queue.CoveragePartial {
		t.Fatalf("host list page = %+v, error=%v", page, err)
	}
	ref := queue.Ref{SourceID: source.ID, ItemID: "sample-1"}
	outcomes := adapter.ReadItems(context.Background(), resolved, []queue.Ref{ref}, []string{"body"}, 1)
	if len(outcomes) != 1 || outcomes[0].Kind != "found" || outcomes[0].Item == nil || outcomes[0].Item.Body == "" {
		t.Fatalf("host read item = %+v", outcomes)
	}
	dependencies, err := adapter.ReadDependencies(context.Background(), resolved, ref, "", 2)
	if err != nil || len(dependencies.Edges) != 2 || dependencies.Completeness != queue.CoverageComplete {
		t.Fatalf("host dependencies = %+v, error=%v", dependencies, err)
	}
	prerequisite := queue.Ref{SourceID: source.ID, ItemID: "sample-3"}
	prerequisiteDeps, err := adapter.ReadDependencies(context.Background(), resolved, prerequisite, "", 2)
	if err != nil || len(prerequisiteDeps.Edges) != 0 {
		t.Fatalf("prerequisite must not emit the dependent's hard edge: %+v, error=%v", prerequisiteDeps, err)
	}
	if _, err := adapter.Capabilities(context.Background(), resolved, "", &ref); err != nil {
		t.Fatalf("host capabilities: %v", err)
	}
}

func TestSampleAdapterProcessHelper(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) || os.Args[separator+1] != "sample-adapter-process" {
		return
	}
	if err := Run(os.Stdin, os.Stdout); err != nil {
		t.Fatalf("serve external sample adapter: %v", err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestSampleAdapterRejectsDuplicateJSONKeys(t *testing.T) {
	t.Parallel()
	client, host := net.Pipe()
	adapter, err := newServer(host)
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- adapter.serve(host) }()
	defer func() {
		_ = client.Close()
		if err := <-serveDone; err != nil {
			t.Errorf("serve adapter: %v", err)
		}
	}()
	if _, err := io.WriteString(client, `{"jsonrpc":"2.0","jsonrpc":"2.0","id":"1","method":"initialize","params":{"protocolMajors":[1],"hostFeatures":[]}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	response, err := bufio.NewReader(client).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatal(err)
	}
	failure := decoded["error"].(map[string]any)
	if failure["code"] != float64(-32700) || failure["data"].(map[string]any)["diagnostic"] != "parse-error" {
		t.Fatalf("duplicate-key response = %#v", decoded)
	}
}
