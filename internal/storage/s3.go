package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

type Client struct {
	s3     *s3.Client
	bucket string
}

func New(ctx context.Context) (*Client, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	bucket := os.Getenv("MINIO_BUCKET")
	access := os.Getenv("MINIO_ACCESS_KEY")
	secret := os.Getenv("MINIO_SECRET_KEY")
	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{URL: fmt.Sprintf("http://%s", endpoint),
			HostnameImmutable: true}, nil
	})
	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(access,
			secret,
			"")),
		config.WithEndpointResolverWithOptions(resolver),
	)
	if err != nil {
		return nil, err
	}
	return &Client{s3: s3.NewFromConfig(cfg), bucket: bucket}, nil
}

func (c *Client) PutJSON(ctx context.Context, v any) (string, error) {
	key := fmt.Sprintf("batches/%s.json", uuid.New().String())
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         &key,
		Body:        bytes.NewReader(b),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("s3://%s/%s", c.bucket, key), nil
}

func parseS3Ref(ref string) (string, string, error) {
	const p = "s3://"
	if !strings.HasPrefix(ref, p) {
		return "", "", fmt.Errorf("bad s3 ref (missing s3://): %q", ref)
	}
	s := strings.TrimPrefix(ref, p)
	slash := strings.IndexByte(s, '/')
	if slash <= 0 || slash == len(s)-1 {
		return "", "", fmt.Errorf("bad s3 ref (need bucket/key): %q", ref)
	}
	return s[:slash], s[slash+1:], nil
}

func (c *Client) GetJSON(ctx context.Context, ref string) (map[string]any, error) {
	_, key, err := parseS3Ref(ref)
	if err != nil {
		return nil, err
	}
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	if err != nil {
		log.Printf("failed to get s3 object %s: %v", ref, err)
		return nil, err
	}
	defer out.Body.Close()
	var v map[string]any
	if err := json.NewDecoder(out.Body).Decode(&v); err != nil {
		log.Printf("failed to decode s3 object %s: %v", ref, err)
		return nil, err
	}
	log.Println("fetched s3 object", ref)
	return v, nil
}
