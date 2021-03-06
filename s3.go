package desync

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	minio "github.com/minio/minio-go"
	"github.com/pkg/errors"
)

// S3Store is a read-write store with S3 backing
type S3Store struct {
	Location string
	client   *minio.Client
	bucket   string
	prefix   string
}

// NewS3Store creates an instance of a chunk store with S3 backing. The URL
// should be provided like this: s3+http://host:port/bucket
// Credentials are passed in via the environment variables S3_ACCESS_KEY
// and S3S3_SECRET_KEY.
func NewS3Store(location string) (S3Store, error) {
	s := S3Store{Location: location}
	u, err := url.Parse(location)
	if err != nil {
		return s, err
	}
	if !strings.HasPrefix(u.Scheme, "s3+http") {
		return s, fmt.Errorf("invalid scheme '%s', expected 's3+http' or 's3+https'", u.Scheme)
	}
	var useSSL bool
	if strings.HasSuffix(u.Scheme, "s") {
		useSSL = true
	}

	// Pull the bucket as well as the prefix from a path-style URL
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return s, fmt.Errorf("expected bucket name in path of '%s'", u.Scheme)
	}
	f := strings.Split(path, "/")
	s.bucket = f[0]
	s.prefix = filepath.Join(f[1:]...)

	// Read creds from the environment and setup a client
	accessKey := os.Getenv("S3_ACCESS_KEY")
	secretKey := os.Getenv("S3_SECRET_KEY")

	s.client, err = minio.New(u.Host, accessKey, secretKey, useSSL)
	if err != nil {
		return s, errors.Wrap(err, location)
	}

	// Might as well confirm the bucket exists
	bucketExists, err := s.client.BucketExists(s.bucket)
	if err != nil {
		return s, errors.Wrap(err, location)
	}
	if !bucketExists {
		return s, fmt.Errorf("bucket '%s' does not exist in %s", s.bucket, location)
	}
	return s, nil
}

// GetChunk reads and returns one (compressed!) chunk from the store
func (s S3Store) GetChunk(id ChunkID) ([]byte, error) {
	name := strings.Join([]string{s.prefix, id.String()}, "/")
	obj, err := s.client.GetObject(s.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, errors.Wrap(err, s.String())
	}
	defer obj.Close()

	b, err := ioutil.ReadAll(obj)
	if err != nil {
		if e, ok := err.(minio.ErrorResponse); ok && e.StatusCode == http.StatusNotFound {
			return nil, ChunkMissing{ID: id}
		}
	}
	return b, err
}

// StoreChunk adds a new chunk to the store
func (s S3Store) StoreChunk(id ChunkID, b []byte) error {
	contentType := "application/zstd"
	name := strings.Join([]string{s.prefix, id.String()}, "/")
	_, err := s.client.PutObject(s.bucket, name, bytes.NewReader(b), int64(len(b)), minio.PutObjectOptions{ContentType: contentType})
	return errors.Wrap(err, s.String())
}

// HasChunk returns true if the chunk is in the store
func (s S3Store) HasChunk(id ChunkID) bool {
	name := strings.Join([]string{s.prefix, id.String()}, "/")
	_, err := s.client.StatObject(s.bucket, name, minio.StatObjectOptions{})
	return err == nil
}

func (s S3Store) String() string {
	return s.Location
}

func (s S3Store) Close() error { return nil }
