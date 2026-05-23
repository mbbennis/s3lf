package s3fs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Client is the AWS-backed FS implementation. Holds one S3 client and an
// in-memory map of bucket→region resolutions. A bucket-region miss returns
// a fresh per-bucket client via the regional cache.
type Client struct {
	base       *s3.Client
	awsCfg     aws.Config
	regionMu   sync.Mutex
	regions    map[string]string
	regional   map[string]*s3.Client
}

func NewClient(awsCfg aws.Config) *Client {
	return &Client{
		base:     s3.NewFromConfig(awsCfg),
		awsCfg:   awsCfg,
		regions:  map[string]string{},
		regional: map[string]*s3.Client{},
	}
}

func (c *Client) ListBuckets(ctx context.Context) ([]Bucket, error) {
	out, err := c.base.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}
	buckets := make([]Bucket, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		name := aws.ToString(b.Name)
		created := time.Time{}
		if b.CreationDate != nil {
			created = *b.CreationDate
		}
		buckets = append(buckets, Bucket{Name: name, Created: created})
	}
	return buckets, nil
}

func (c *Client) Region(ctx context.Context, bucket string) (string, error) {
	c.regionMu.Lock()
	if r, ok := c.regions[bucket]; ok {
		c.regionMu.Unlock()
		return r, nil
	}
	c.regionMu.Unlock()

	out, err := c.base.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return "", err
	}
	region := string(out.LocationConstraint)
	if region == "" {
		// us-east-1 reports empty LocationConstraint.
		region = "us-east-1"
	}
	c.regionMu.Lock()
	c.regions[bucket] = region
	c.regionMu.Unlock()
	return region, nil
}

func (c *Client) clientFor(ctx context.Context, bucket string) (*s3.Client, error) {
	region, err := c.Region(ctx, bucket)
	if err != nil {
		return nil, err
	}
	c.regionMu.Lock()
	defer c.regionMu.Unlock()
	if cl, ok := c.regional[region]; ok {
		return cl, nil
	}
	cfg := c.awsCfg.Copy()
	cfg.Region = region
	cl := s3.NewFromConfig(cfg)
	c.regional[region] = cl
	return cl, nil
}

func (c *Client) List(ctx context.Context, bucket, prefix, token string) (*Listing, error) {
	cl, err := c.clientFor(ctx, bucket)
	if err != nil {
		return nil, err
	}
	in := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	}
	if token != "" {
		in.ContinuationToken = aws.String(token)
	}
	out, err := cl.ListObjectsV2(ctx, in)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(out.CommonPrefixes)+len(out.Contents))
	for _, cp := range out.CommonPrefixes {
		full := aws.ToString(cp.Prefix)
		// CommonPrefix is "<current-prefix><dirname>/"; strip both for display.
		name := full
		if len(name) > len(prefix) {
			name = name[len(prefix):]
		}
		name = trimTrailingSlash(name)
		if name == "" {
			continue
		}
		entries = append(entries, Entry{Name: name, IsDir: true})
	}
	for _, o := range out.Contents {
		key := aws.ToString(o.Key)
		// A "directory marker" object — zero-byte key ending in /. Skip; the
		// equivalent CommonPrefix already covers it.
		if len(key) > 0 && key[len(key)-1] == '/' {
			continue
		}
		name := key
		if len(name) > len(prefix) {
			name = name[len(prefix):]
		}
		size := int64(0)
		if o.Size != nil {
			size = *o.Size
		}
		mod := time.Time{}
		if o.LastModified != nil {
			mod = *o.LastModified
		}
		entries = append(entries, Entry{
			Name:     name,
			Size:     size,
			Modified: mod,
			Storage:  string(o.StorageClass),
		})
	}

	next := aws.ToString(out.NextContinuationToken)
	return &Listing{
		Bucket:    bucket,
		Prefix:    prefix,
		Entries:   entries,
		NextToken: next,
		Complete:  next == "",
		FetchedAt: time.Now(),
	}, nil
}

func trimTrailingSlash(s string) string {
	if n := len(s); n > 0 && s[n-1] == '/' {
		return s[:n-1]
	}
	return s
}

func (c *Client) HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error) {
	cl, err := c.clientFor(ctx, bucket)
	if err != nil {
		return nil, err
	}
	out, err := cl.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	info := &ObjectInfo{
		ETag:        strings.Trim(aws.ToString(out.ETag), `"`),
		ContentType: aws.ToString(out.ContentType),
	}
	if out.ContentLength != nil {
		info.Size = *out.ContentLength
	}
	if out.LastModified != nil {
		info.Modified = *out.LastModified
	}
	return info, nil
}

func (c *Client) Download(ctx context.Context, bucket, key string, w io.WriterAt, ifMatch string) (int64, error) {
	cl, err := c.clientFor(ctx, bucket)
	if err != nil {
		return 0, err
	}
	tm := transfermanager.New(cl)
	in := &transfermanager.DownloadObjectInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		WriterAt: w,
	}
	if ifMatch != "" {
		in.IfMatch = aws.String(`"` + ifMatch + `"`)
	}
	out, err := tm.DownloadObject(ctx, in)
	if err != nil {
		if isPreconditionFailed(err) {
			return 0, &preconditionErr{wrapped: err}
		}
		return 0, err
	}
	if out.ContentLength != nil {
		return *out.ContentLength, nil
	}
	return 0, nil
}

func (c *Client) Upload(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType, ifMatch string) error {
	cl, err := c.clientFor(ctx, bucket)
	if err != nil {
		return err
	}
	tm := transfermanager.New(cl)
	in := &transfermanager.UploadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   r,
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	if ifMatch != "" {
		in.IfMatch = aws.String(`"` + ifMatch + `"`)
	}
	_, err = tm.UploadObject(ctx, in)
	if err != nil && isPreconditionFailed(err) {
		return &preconditionErr{wrapped: err}
	}
	return err
}

func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	cl, err := c.clientFor(ctx, bucket)
	if err != nil {
		return err
	}
	_, err = cl.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

func isPreconditionFailed(err error) bool {
	var re *smithyhttp.ResponseError
	if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusPreconditionFailed {
		return true
	}
	return false
}

