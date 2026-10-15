package inspectors

import (
	"bytes"
	"strings"
	"testing"

	"github.com/theazz/awless-ro/cloud/properties"
	"github.com/theazz/awless-ro/graph"
	"github.com/theazz/awless-ro/graph/resourcetest"
)

// An empty graph is what a fresh install has, because object sync is off by default:
// one API call per bucket is too much to spend unasked. The inspector used to print a
// table whose only row read "0.000000 Gb", which answers a question about the account
// that was never looked into.
func TestBucketSizerSaysNothingWasFetchedRatherThanZero(t *testing.T) {
	var out bytes.Buffer
	sizer := &BucketSizer{}

	if err := sizer.Inspect(graph.NewGraph()); err != nil {
		t.Fatal(err)
	}
	sizer.Print(&out)

	got := out.String()
	if strings.Contains(got, "0.000000 Gb") {
		t.Errorf("a total of zero was reported for an account nothing was read from:\n%s", got)
	}
	// And it has to say what to do about it.
	if !strings.Contains(got, "aws.storage.s3object.sync") {
		t.Errorf("the setting that turns object sync on is not mentioned:\n%s", got)
	}
}

// Buckets synced with object sync off carry no totals: that is "never looked", not
// "empty".
func TestBucketSizerSaysNothingWasFetchedForBucketsWithoutTotals(t *testing.T) {
	g := graph.NewGraph()
	g.AddResource(resourcetest.Bucket("logs").Build())

	var out bytes.Buffer
	sizer := &BucketSizer{}
	if err := sizer.Inspect(g); err != nil {
		t.Fatal(err)
	}
	sizer.Print(&out)
	if got := out.String(); !strings.Contains(got, "aws.storage.s3object.sync") || strings.Contains(got, "TOTAL") {
		t.Errorf("a bucket that was never sized is reported as sized:\n%s", got)
	}
}

// The sizes come from the totals a sync keeps on each bucket. Objects kept in the
// graph as well (aws.storage.s3object.keep) are not counted a second time.
func TestBucketSizerReadsTheTotalsOnEachBucket(t *testing.T) {
	g := graph.NewGraph()
	g.AddResource(
		sized("logs", 2, 2_000_000_000),
		sized("backups", 1, 3_000_000_000),
		resourcetest.S3Object("obj_1").Prop(properties.Bucket, "logs").Prop(properties.Size, 1_500_000_000).Build(),
	)

	var out bytes.Buffer
	sizer := &BucketSizer{}
	if err := sizer.Inspect(g); err != nil {
		t.Fatal(err)
	}
	sizer.Print(&out)
	got := out.String()

	for _, want := range []string{
		"backups", "3.000000 Gb",
		"logs", "2.000000 Gb",
		"TOTAL", "5.000000 Gb",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q:\n%s", want, got)
		}
	}

	if got, want := sizer.buckets["logs"].objects, 2; got != want {
		t.Errorf("logs holds %d objects, want %d", got, want)
	}
}

// Rows are ordered, because map order printed the same data differently on every run,
// which cannot be compared against an earlier report.
func TestBucketSizerOutputIsOrdered(t *testing.T) {
	g := graph.NewGraph()
	g.AddResource(
		sized("zebra", 1, 1),
		sized("alpha", 1, 1),
		sized("middle", 1, 1),
	)

	var first string
	for range 5 {
		var out bytes.Buffer
		sizer := &BucketSizer{}
		if err := sizer.Inspect(g); err != nil {
			t.Fatal(err)
		}
		sizer.Print(&out)

		if first == "" {
			first = out.String()
			continue
		}
		if out.String() != first {
			t.Fatalf("two runs over the same graph printed different output:\n%s\nand\n%s", first, out.String())
		}
	}

	alpha, zebra := strings.Index(first, "alpha"), strings.Index(first, "zebra")
	if alpha > zebra {
		t.Errorf("rows are not in name order:\n%s", first)
	}
}

// The property reads are checked, not asserted: an inspector reports on whatever the
// graph happens to hold, and an unchecked assertion took the process down.
func TestBucketSizerSurvivesOddProperties(t *testing.T) {
	g := graph.NewGraph()
	g.AddResource(
		sized("logs", 3, 1_000_000_000),
		// A size that is not an int: the objects count, the size does not.
		resourcetest.Bucket("odd").Prop(properties.ObjectCount, 4).Prop(properties.Size, "not a number").Build(),
		// A count that is not an int: not sized.
		resourcetest.Bucket("odder").Prop(properties.ObjectCount, "many").Build(),
		// No totals at all: never sized.
		resourcetest.Bucket("unsized").Build(),
	)

	var out bytes.Buffer
	sizer := &BucketSizer{}
	if err := sizer.Inspect(g); err != nil {
		t.Fatal(err)
	}
	sizer.Print(&out)

	if sizer.total != 1_000_000_000 {
		t.Errorf("total is %d bytes, want 1000000000:\n%s", sizer.total, out.String())
	}
	if odd := sizer.buckets["odd"]; odd == nil || odd.objects != 4 {
		t.Errorf("odd should hold 4 objects, got %+v", odd)
	}
	for _, name := range []string{"odder", "unsized"} {
		if _, ok := sizer.buckets[name]; ok {
			t.Errorf("%s has no count, yet has a row", name)
		}
	}
}

// A bucket whose listing failed, or never ran because an earlier one failed, has no
// count. Left out silently, the total would read as the account's.
func TestBucketSizerSaysWhichBucketsWereNotSized(t *testing.T) {
	g := graph.NewGraph()
	g.AddResource(
		sized("logs", 2, 2_000_000_000),
		resourcetest.Bucket("denied").Build(),
	)

	sizer := &BucketSizer{}
	if err := sizer.Inspect(g); err != nil {
		t.Fatal(err)
	}
	var out, notes bytes.Buffer
	sizer.Print(&out)
	sizer.Rows(&notes)

	for name, got := range map[string]string{"text": out.String(), "notes": notes.String()} {
		if !strings.Contains(got, "1 bucket(s) have no size") {
			t.Errorf("%s: the unsized bucket is not reported:\n%s", name, got)
		}
	}
}

// A graph synced by an earlier version holds the objects and no totals. Object sync is
// on there, so "turn it on" would be the wrong advice.
func TestBucketSizerOnAGraphFromAnEarlierVersion(t *testing.T) {
	g := graph.NewGraph()
	g.AddResource(
		resourcetest.Bucket("logs").Build(),
		resourcetest.S3Object("obj_1").Prop(properties.Bucket, "logs").Prop(properties.Size, 1).Build(),
	)

	sizer := &BucketSizer{}
	if err := sizer.Inspect(g); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	sizer.Print(&out)
	if got := out.String(); !strings.Contains(got, "sync --storage") || strings.Contains(got, "config set aws.storage.s3object.sync") {
		t.Errorf("want advice to sync again, got:\n%s", got)
	}
}

func sized(name string, objects, size int) *graph.Resource {
	return resourcetest.Bucket(name).
		Prop(properties.ObjectCount, objects).
		Prop(properties.Size, size).
		Build()
}
