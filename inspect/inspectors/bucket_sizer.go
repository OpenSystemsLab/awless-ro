/*
Copyright 2017 WALLIX

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package inspectors

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/theazz/awless-ro/cloud"
	"github.com/theazz/awless-ro/cloud/properties"
)

type BucketSizer struct {
	total   int
	buckets map[string]*bucket

	// unsized counts the buckets with no count on them, whose listing failed or
	// never ran. Reported, so that the total is not read as the account's.
	unsized int

	// objectsInGraph is only looked at when no bucket was sized: objects with no
	// totals are a graph synced by an earlier version, with object sync already on.
	objectsInGraph int
}

type bucket struct {
	objects, size int
}

func (*BucketSizer) Name() string {
	return "bucket_sizer"
}

// Inspect reads the totals a sync keeps on each bucket: the number of objects and
// their size in bytes. It used to add up the objects themselves, which meant keeping
// every object of the account in the graph. A bucket with no count was never sized
// and gets no row rather than a zero.
func (i *BucketSizer) Inspect(g cloud.GraphAPI) error {
	i.buckets = make(map[string]*bucket)

	buckets, err := g.Find(cloud.NewQuery(cloud.Bucket))
	if err != nil {
		return err
	}

	for _, b := range buckets {
		// Read rather than asserted: an inspector reports on whatever the graph
		// happens to hold.
		count, ok := b.Properties()[properties.ObjectCount].(int)
		if !ok {
			i.unsized++
			continue
		}
		size, _ := b.Properties()[properties.Size].(int)

		i.total = i.total + size
		i.buckets[b.Id()] = &bucket{objects: count, size: size}
	}

	if len(i.buckets) == 0 {
		objects, err := g.Find(cloud.NewQuery(cloud.S3Object))
		if err != nil {
			return err
		}
		i.objectsInGraph = len(objects)
	}

	return nil
}

// BucketSize is one row of `inspect -i bucket_sizer --format json`.
type BucketSize struct {
	Bucket  string `json:"bucket"`
	Objects int    `json:"objects"`
	Bytes   int    `json:"bytes"`
}

// Rows are the buckets in name order, sizes in bytes. The total is their sum and is
// not repeated as a row of its own.
func (i *BucketSizer) Rows(notes io.Writer) interface{} {
	rows := []BucketSize{}
	if len(i.buckets) == 0 {
		i.printNothingFetched(notes)
		return rows
	}
	for _, name := range i.sortedNames() {
		rows = append(rows, BucketSize{Bucket: name, Objects: i.buckets[name].objects, Bytes: i.buckets[name].size})
	}
	i.printUnsized(notes)
	return rows
}

// printNothingFetched says why there is nothing to size. No bucket sized at all is
// the state a fresh install is in, because object sync is off by default: one API call
// per bucket is too much to spend unasked. Printing a table whose only row read
// "0.000000 Gb" answered a question about the account that had not been looked into.
func (i *BucketSizer) printNothingFetched(w io.Writer) {
	if i.objectsInGraph > 0 {
		fmt.Fprintln(w, "The local graph holds S3 objects but no bucket sizes: it was synced by an earlier version. Sync it again with:")
		fmt.Fprintln(w, "\tawless-ro sync --storage")
		return
	}
	fmt.Fprintln(w, "No bucket in the local graph has been sized, so there is nothing to report.")
	fmt.Fprintln(w, "Object sync is off by default because it costs one API call per bucket. Turn it on with:")
	fmt.Fprintln(w, "\tawless-ro config set aws.storage.s3object.sync true")
}

func (i *BucketSizer) printUnsized(w io.Writer) {
	if i.unsized > 0 {
		fmt.Fprintf(w, "\n%d bucket(s) have no size, because their listing failed or never ran; the total leaves them out.\n", i.unsized)
	}
}

// sortedNames is the buckets in name order, because map order made the same data
// print in a different order every run, which is unreadable for a report meant to be
// compared against an earlier one.
func (i *BucketSizer) sortedNames() []string {
	names := make([]string, 0, len(i.buckets))
	for name := range i.buckets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (i *BucketSizer) Print(w io.Writer) {
	if len(i.buckets) == 0 {
		i.printNothingFetched(w)
		return
	}

	tabw := tabwriter.NewWriter(w, 0, 8, 0, '\t', 0)

	fmt.Fprintln(tabw, "Bucket\tObject count\tS3 total storage\t")
	fmt.Fprintln(tabw, "--------\t----------\t-----------------\t")

	for _, name := range i.sortedNames() {
		bucket := i.buckets[name]
		fmt.Fprintf(tabw, "%s\t%d\t%0.6f Gb\t\n", name, bucket.objects, float64(bucket.size)/1e9)
	}

	fmt.Fprintf(tabw, "%s\t%s\t%0.6f Gb\t\n", "TOTAL", "", float64(i.total)/1e9)

	tabw.Flush()

	i.printUnsized(w)
}
