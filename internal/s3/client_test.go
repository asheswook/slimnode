package s3_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/s3"
)

type listBucketResult struct {
	XMLName               xml.Name    `xml:"ListBucketResult"`
	Xmlns                 string      `xml:"xmlns,attr"`
	Contents              []s3Object  `xml:"Contents"`
	IsTruncated           bool        `xml:"IsTruncated"`
	NextContinuationToken string      `xml:"NextContinuationToken,omitempty"`
}

type s3Object struct {
	Key  string `xml:"Key"`
	Size int64  `xml:"Size"`
}

func newTestClient(t *testing.T, srv *httptest.Server, opts ...s3.Option) *s3.Client {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	baseOpts := []s3.Option{
		s3.WithEndpoint(srv.URL),
		s3.WithPathStyle(true),
		s3.WithRegion("us-east-1"),
	}
	baseOpts = append(baseOpts, opts...)
	c, err := s3.New(context.Background(), "test-bucket", baseOpts...)
	require.NoError(t, err)
	return c
}

func writeXML(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_ = xml.NewEncoder(w).Encode(v)
}

func TestUpload(t *testing.T) {
	var gotMethod, gotPath, gotCacheControl, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCacheControl = r.Header.Get("Cache-Control")
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)

	body := bytes.NewReader([]byte("hello world"))
	err := c.Upload(context.Background(), "blocks/blk00001.dat", body, int64(len("hello world")))
	require.NoError(t, err)

	assert.Equal(t, http.MethodPut, gotMethod)
	assert.Equal(t, "/test-bucket/blocks/blk00001.dat", gotPath)
	assert.Equal(t, "public, max-age=31536000, immutable", gotCacheControl)
	assert.Equal(t, "application/octet-stream", gotContentType)
}

func TestUploadStorageClass(t *testing.T) {
	var gotStorageClass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotStorageClass = r.Header.Get("x-amz-storage-class")
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, s3.WithStorageClass("STANDARD"))

	body := bytes.NewReader([]byte("hello world"))
	err := c.Upload(context.Background(), "blocks/blk00001.dat", body, int64(len("hello world")))
	require.NoError(t, err)

	assert.Equal(t, "STANDARD", gotStorageClass)
}

func TestHead_Exists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "12345")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)

	size, exists, err := c.Head(context.Background(), "blocks/blk00001.dat")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, int64(12345), size)
}

func TestHead_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)

	size, exists, err := c.Head(context.Background(), "blocks/blk99999.dat")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, int64(0), size)
}

func TestList(t *testing.T) {
	result := listBucketResult{
		Xmlns: "http://s3.amazonaws.com/doc/2006-03-01/",
		Contents: []s3Object{
			{Key: "blocks/blk00000.dat", Size: 100},
			{Key: "blocks/blk00001.dat", Size: 200},
		},
		IsTruncated: false,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeXML(w, result)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)

	keys, err := c.List(context.Background(), "blocks/")
	require.NoError(t, err)
	assert.Equal(t, []string{"blocks/blk00000.dat", "blocks/blk00001.dat"}, keys)
}

func TestList_Pagination(t *testing.T) {
	page1 := listBucketResult{
		Xmlns:                 "http://s3.amazonaws.com/doc/2006-03-01/",
		Contents:              []s3Object{{Key: "blocks/blk00000.dat", Size: 100}},
		IsTruncated:           true,
		NextContinuationToken: "token1",
	}
	page2 := listBucketResult{
		Xmlns:       "http://s3.amazonaws.com/doc/2006-03-01/",
		Contents:    []s3Object{{Key: "blocks/blk00001.dat", Size: 200}},
		IsTruncated: false,
	}
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 1 {
			writeXML(w, page1)
		} else {
			writeXML(w, page2)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)

	keys, err := c.List(context.Background(), "blocks/")
	require.NoError(t, err)
	assert.Equal(t, []string{"blocks/blk00000.dat", "blocks/blk00001.dat"}, keys)
	assert.Equal(t, 2, call)
}

func TestDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)

	err := c.Delete(context.Background(), "blocks/blk00001.dat")
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, gotMethod)
	assert.Equal(t, "/test-bucket/blocks/blk00001.dat", gotPath)
}
