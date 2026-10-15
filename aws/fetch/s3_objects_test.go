package awsfetch

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/theazz/awless-ro/cloud"
	"github.com/theazz/awless-ro/cloud/properties"
	"github.com/theazz/awless-ro/fetch"
	"github.com/theazz/awless-ro/graph"
)

// pagedS3 lists each bucket's objects a page at a time, as S3 does past 1000 keys,
// and can fail on a given page of a given bucket.
type pagedS3 struct {
	stubS3
	pageSize   int
	failBucket string
	failPage   int
}

func (p *pagedS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	name := awssdk.ToString(in.Bucket)
	page := tokenIndex(awssdk.ToString(in.ContinuationToken))
	if name == p.failBucket && page == p.failPage {
		return nil, errors.New("listing failed")
	}
	all := p.objects[name]
	start, end := page*p.pageSize, (page+1)*p.pageSize
	if end > len(all) {
		end = len(all)
	}
	out := &s3.ListObjectsV2Output{Contents: all[start:end], IsTruncated: awssdk.Bool(end < len(all))}
	if end < len(all) {
		out.NextContinuationToken = awssdk.String(strconv.Itoa(page + 1))
	}
	return out, nil
}

func objectsOfSize(n int, size int64) []s3types.Object {
	objects := make([]s3types.Object, n)
	for i := range objects {
		objects[i] = s3types.Object{Key: awssdk.String(fmt.Sprintf("key-%d", i)), Size: awssdk.Int64(size)}
	}
	return objects
}

func newPagedS3() *pagedS3 {
	return &pagedS3{
		pageSize: 1000,
		stubS3: stubS3{
			buckets: map[string][]s3types.Bucket{"eu-west-1": {{Name: awssdk.String("logs")}, {Name: awssdk.String("empty")}}},
			objects: map[string][]s3types.Object{"logs": objectsOfSize(2500, 3)},
			grants:  map[string][]s3types.Grant{"logs": {{Permission: "READ", Grantee: &s3types.Grantee{ID: awssdk.String("owner"), Type: "CanonicalUser"}}}},
		},
		failPage: -1,
	}
}

func fetchObjects(t *testing.T, api S3API, extra map[string]interface{}, ctx context.Context) []*graph.Resource {
	t.Helper()
	conf := NewConfig(&AWSAPI{S3: api})
	conf.Extra["aws.storage.s3object.sync"] = true
	for k, v := range extra {
		conf.Extra[k] = v
	}
	funcs := BuildStorageFetchFuncs(conf)
	resources, _, err := funcs["s3object"](context.WithValue(ctx, "region", "eu-west-1"), fetch.NewFetcher(funcs))
	if err != nil {
		t.Fatal(err)
	}
	return resources
}

func byType(resources []*graph.Resource, typ string) []*graph.Resource {
	var out []*graph.Resource
	for _, r := range resources {
		if r.Type() == typ {
			out = append(out, r)
		}
	}
	return out
}

// Object sync used to keep every object of every bucket in memory until the end of
// the sync, and an account with a few busy buckets took a 512 MiB container to its
// limit in seconds. bucket_sizer, the only reader, needs two numbers per bucket.
func TestObjectSyncKeepsPerBucketTotalsNotObjects(t *testing.T) {
	resources := fetchObjects(t, newPagedS3(), nil, context.Background())

	if objects := byType(resources, cloud.S3Object); len(objects) != 0 {
		t.Errorf("got %d object resources, want none: memory would grow with the object count", len(objects))
	}

	buckets := byType(resources, cloud.Bucket)
	totals := map[string][2]interface{}{}
	for _, b := range buckets {
		if _, seen := totals[b.Id()]; seen {
			t.Errorf("bucket %s was sent more than once", b.Id())
		}
		totals[b.Id()] = [2]interface{}{b.Properties()[properties.ObjectCount], b.Properties()[properties.Size]}
	}
	want := map[string][2]interface{}{
		"logs":  {2500, 7500},
		"empty": {0, 0},
	}
	if fmt.Sprint(totals) != fmt.Sprint(want) {
		t.Errorf("got totals %v, want %v", totals, want)
	}
}

// The totals go into the graph on the bucket the bucket fetcher builds, not on a
// second node.
func TestObjectTotalsLandOnTheSyncedBucket(t *testing.T) {
	conf := NewConfig(&AWSAPI{S3: newPagedS3()})
	conf.Extra["aws.storage.s3object.sync"] = true
	ctx := context.WithValue(context.Background(), "region", "eu-west-1")

	g, err := fetch.NewFetcher(BuildStorageFetchFuncs(conf)).Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := g.GetResource(cloud.Bucket, "logs")
	if err != nil {
		t.Fatal(err)
	}
	if got := logs.Properties()[properties.ObjectCount]; got != 2500 {
		t.Errorf("object count %v, want 2500", got)
	}
	if got := logs.Properties()[properties.Size]; got != 7500 {
		t.Errorf("size %v, want 7500", got)
	}
	if _, ok := logs.Properties()[properties.Grants]; !ok {
		t.Error("the bucket lost what the bucket fetcher put on it")
	}
}

// Keeping the objects themselves stays possible, for `list s3objects --local` on small
// buckets. Each one still points at its bucket, which is built once.
func TestObjectSyncKeepsObjectsWhenAskedTo(t *testing.T) {
	resources := fetchObjects(t, newPagedS3(), map[string]interface{}{"aws.storage.s3object.keep": true}, context.Background())

	objects := byType(resources, cloud.S3Object)
	if len(objects) != 2500 {
		t.Fatalf("got %d objects, want 2500", len(objects))
	}
	if got := objects[0].Properties()[properties.Bucket]; got != "logs" {
		t.Errorf("object names bucket %v, want logs", got)
	}
	if n := len(byType(resources, cloud.Bucket)); n != 2 {
		t.Errorf("got %d bucket resources, want one per bucket", n)
	}
}

// `list s3objects` without --local asks AWS for the objects and prints them, so they
// have to come back whatever the sync setting says.
func TestLiveObjectListingReturnsObjects(t *testing.T) {
	conf := NewConfig(&AWSAPI{S3: newPagedS3()})
	ctx := context.WithValue(context.Background(), "region", "eu-west-1")
	ctx = context.WithValue(ctx, "force", true)

	g, err := fetch.NewFetcher(BuildStorageFetchFuncs(conf)).FetchByType(ctx, cloud.S3Object)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := g.GetAllResources(cloud.S3Object)
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 2500 {
		t.Errorf("got %d objects, want 2500", len(objects))
	}
}

// A listing that fails half way gives no totals for that bucket: a partial count
// would read as the bucket's size.
func TestFailedListingGivesNoPartialTotals(t *testing.T) {
	api := newPagedS3()
	api.failBucket, api.failPage = "logs", 1

	conf := NewConfig(&AWSAPI{S3: api})
	conf.Extra["aws.storage.s3object.sync"] = true
	funcs := BuildStorageFetchFuncs(conf)
	ctx := context.WithValue(context.Background(), "region", "eu-west-1")

	resources, _, err := funcs["s3object"](ctx, fetch.NewFetcher(funcs))
	if err == nil {
		t.Fatal("the listing error was swallowed")
	}
	for _, b := range byType(resources, cloud.Bucket) {
		if n, ok := b.Properties()[properties.ObjectCount]; ok && b.Id() == "logs" {
			t.Errorf("logs got a count of %v from a listing that failed", n)
		}
	}
}
