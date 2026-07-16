package seaweedfs

import (
	"context"
	"fmt"
	"io"

	"github.com/alexonderia/filestore/internal/config"
	"github.com/alexonderia/filestore/internal/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Store struct {
	bucket   string
	client   *s3.Client
	transfer *transfermanager.Client
}

func New(ctx context.Context, cfg config.API) (*Store, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.S3Region)}
	if cfg.S3AccessKey != "" {
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, "")))
	} else {
		options = append(options, awsconfig.WithCredentialsProvider(aws.AnonymousCredentials{}))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.S3Endpoint)
		options.UsePathStyle = true
	})
	store := &Store{bucket: cfg.S3Bucket, client: client}
	store.transfer = transfermanager.New(client, func(options *transfermanager.Options) {
		options.PartSizeBytes = 8 * 1024 * 1024
		options.Concurrency = 2
	})
	return store, nil
}

func (store *Store) EnsureBucket(ctx context.Context) error {
	if _, err := store.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(store.bucket)}); err == nil {
		return nil
	}
	if _, err := store.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(store.bucket)}); err != nil {
		return fmt.Errorf("ensure S3 bucket: %w", err)
	}
	return nil
}

func (store *Store) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	_, err := store.transfer.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(store.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put S3 object: %w", err)
	}
	return nil
}

func (store *Store) Get(ctx context.Context, key string) (storage.Object, error) {
	result, err := store.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)})
	if err != nil {
		return storage.Object{}, fmt.Errorf("get S3 object: %w", err)
	}
	return storage.Object{Body: result.Body, Size: aws.ToInt64(result.ContentLength), ContentType: aws.ToString(result.ContentType)}, nil
}

func (store *Store) Delete(ctx context.Context, key string) error {
	if _, err := store.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)}); err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}

func (store *Store) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(store.client, &s3.ListObjectsV2Input{Bucket: aws.String(store.bucket), Prefix: aws.String(prefix)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list S3 objects: %w", err)
		}
		for _, object := range page.Contents {
			keys = append(keys, aws.ToString(object.Key))
		}
	}
	return keys, nil
}
