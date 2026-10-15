package awsfetch

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/theazz/awless-ro/aws/conv"
	"github.com/theazz/awless-ro/cloud/properties"
	"github.com/theazz/awless-ro/cloud/rdf"
	"github.com/theazz/awless-ro/fetch"
	"github.com/theazz/awless-ro/graph"
)

func forEachBucketParallel(ctx context.Context, cache fetch.Cache, api S3API, f func(b s3types.Bucket) error) error {
	var buckets []s3types.Bucket

	if val, e := cache.Get("getBucketsPerRegion", func() (interface{}, error) {
		return getBucketsPerRegion(ctx, api)
	}); e != nil {
		return e
	} else if v, ok := val.([]s3types.Bucket); ok {
		buckets = v
	}

	// Bounded, because each call goes to the bucket's own hostname: see
	// maxParallelCalls.
	return forEachParallel(ctx, buckets, func(_ context.Context, b s3types.Bucket) error {
		return f(b)
	})
}

// fetchObjectsForBucket pages through a bucket's objects and sends the bucket with
// two totals on it, ObjectCount and Size in bytes, which is all bucket_sizer reads.
//
// It used to send every object as a resource, plus a fresh copy of the bucket per
// object, and a sync held all of them until it wrote the graph: memory grew with the
// number of objects in the account, and a few busy buckets were enough to take a
// 512 MiB container to its limit. The objects themselves are only sent when
// keepObjects is set, for `list s3objects` and whoever turns on
// aws.storage.s3object.keep.
//
// A listing that fails part way sends no totals, since a partial count would read as
// the bucket's size.
func fetchObjectsForBucket(ctx context.Context, api S3API, bucket s3types.Bucket, keepObjects bool, resourcesC chan<- *graph.Resource) error {
	parent, err := awsconv.InitResource(bucket)
	if err != nil {
		return err
	}

	var count, size int
	paginator := s3.NewListObjectsV2Paginator(api, &s3.ListObjectsV2Input{Bucket: bucket.Name})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, output := range page.Contents {
			count++
			size += int(aws.ToInt64(output.Size))
			if !keepObjects {
				continue
			}
			res, err := awsconv.NewResource(output)
			if err != nil {
				return err
			}
			res.SetProperty(properties.Bucket, aws.ToString(bucket.Name))
			res.AddRelation(rdf.ChildrenOfRel, parent)
			resourcesC <- res
		}
	}

	parent.SetProperty(properties.ObjectCount, count)
	parent.SetProperty(properties.Size, size)
	resourcesC <- parent
	return nil
}

// getBucketsPerRegion returns the buckets that live in the region being synced.
//
// ListBuckets lists every bucket in the account, whatever its region. This used to be
// followed by a GetBucketLocation for each of them, all at once: one request per
// bucket in the account, each to the bucket's own hostname, so as many DNS lookups of
// distinct names in the same instant — and the first that failed failed the listing.
// A cold resolver answering one of them "no such host" was enough, and the SDK does
// not retry that. S3 has filtered ListBuckets by region itself since October 2024, so
// this is a single paginated call to the regional endpoint.
func getBucketsPerRegion(ctx context.Context, api S3API) ([]s3types.Bucket, error) {
	region, _ := ctx.Value("region").(string)
	if region == "" {
		// Without a region the filter would be dropped and every bucket in the
		// account returned as if it were local.
		return nil, errors.New("s3: no region to list buckets for")
	}

	var all []s3types.Bucket
	paginator := s3.NewListBucketsPaginator(api, &s3.ListBucketsInput{BucketRegion: aws.String(region)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Buckets...)
	}

	var userBucketName string
	var hasBucketFilter bool
	if id, hasID := getUserFiltersFromContext(ctx)["id"]; hasID {
		userBucketName = id
		hasBucketFilter = true
	} else if buck, hasBucket := getUserFiltersFromContext(ctx)["bucket"]; hasBucket {
		userBucketName = buck
		hasBucketFilter = true
	}
	if !hasBucketFilter {
		return all, nil
	}

	var buckets []s3types.Bucket
	for _, b := range all {
		if strings.Contains(strings.ToLower(aws.ToString(b.Name)), strings.ToLower(userBucketName)) {
			buckets = append(buckets, b)
		}
	}
	return buckets, nil
}

func fetchAndExtractGrantsFn(ctx context.Context, api S3API, bucketName string) ([]*graph.Grant, error) {
	acls, err := api.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: aws.String(bucketName)})
	if err != nil {
		return nil, err
	}
	var grants []*graph.Grant
	for _, acl := range acls.Grants {
		var displayName, granteeType, granteeId string
		if acl.Grantee != nil {
			displayName = aws.ToString(acl.Grantee.DisplayName)
			granteeType = string(acl.Grantee.Type)
			granteeId = aws.ToString(acl.Grantee.ID)

			if aws.ToString(acl.Grantee.EmailAddress) != "" {
				displayName += "<" + aws.ToString(acl.Grantee.EmailAddress) + ">"
			}
			if granteeType == "Group" {
				granteeId += aws.ToString(acl.Grantee.URI)
			}
		}
		grants = append(grants, &graph.Grant{
			Permission: string(acl.Permission),
			Grantee: graph.Grantee{
				GranteeID:          granteeId,
				GranteeType:        granteeType,
				GranteeDisplayName: displayName,
			},
		})
	}
	return grants, nil
}
