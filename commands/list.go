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

package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/theazz/awless-ro/aws/services"
	"github.com/theazz/awless-ro/cloud"
	"github.com/theazz/awless-ro/config"
	"github.com/theazz/awless-ro/console"
	"github.com/theazz/awless-ro/logger"
	"github.com/theazz/awless-ro/sync"
)

var (
	listingFiltersFlag         []string
	listingTagFiltersFlag      []string
	listingTagKeyFiltersFlag   []string
	listingTagValueFiltersFlag []string
	listingColumnsFlag         []string
	listOnlyIDs                bool
	noHeadersFlag              bool
	sortBy                     []string
	reverseFlag                bool
)

func init() {
	RootCmd.AddCommand(listCmd)

	cobra.EnableCommandSorting = false

	for _, srvName := range awsservices.ServiceNames {
		listCmd.AddCommand(listAllResourceInServiceCmd(srvName))
	}

	for _, name := range awsservices.ServiceNames {
		var resources []string
		for _, resType := range awsservices.ResourceTypesPerServiceName()[name] {
			resources = append(resources, resType)
		}
		sort.Strings(resources)
		for _, resType := range resources {
			listCmd.AddCommand(listSpecificResourceCmd(resType))
		}
	}

	listCmd.PersistentFlags().StringSliceVar(&listingFiltersFlag, "filter", []string{}, "Filter resources by column, case insensitive: key=value matches a substring, key==value matches the whole value. Ex: --filter type=t2.micro --filter state==running")
	listCmd.PersistentFlags().StringSliceVar(&listingTagFiltersFlag, "tag", []string{}, "Filter EC2 resources given tags (case sensitive!). Ex: --tag Env=Production")
	listCmd.PersistentFlags().StringSliceVar(&listingTagKeyFiltersFlag, "tag-key", []string{}, "Filter EC2 resources given a tag key only (case sensitive!). Ex: --tag-key Env")
	listCmd.PersistentFlags().StringSliceVar(&listingTagValueFiltersFlag, "tag-value", []string{}, "Filter EC2 resources given a tag value only (case sensitive!). Ex: --tag-value Staging")
	listCmd.PersistentFlags().StringSliceVar(&listingColumnsFlag, "columns", []string{}, "Select the properties to display in the columns. Ex: --columns id,name,cidr")
	listCmd.PersistentFlags().BoolVar(&listOnlyIDs, "ids", false, "List only ids")
	listCmd.PersistentFlags().BoolVar(&noHeadersFlag, "no-headers", false, "Do not display headers")
	listCmd.PersistentFlags().BoolVar(&reverseFlag, "reverse", false, "Use in conjunction with --sort to reverse sort")
	listCmd.PersistentFlags().StringSliceVar(&sortBy, "sort", []string{"Id"}, "Sort tables by column(s) name(s)")
	listCmd.PersistentFlags().Var(&maxWidthFlag, "max-width", maxWidthUsage)
	// A comma inside a tag value splits it in two (#28): refuse that before any AWS call.
	guardTagFilterFlags(listCmd)
}

var listCmd = &cobra.Command{
	Use:               "list",
	Aliases:           []string{"ls"},
	Example:           "  awless-ro list instances --sort uptime\n  awless-ro list users --format csv\n  awless-ro list volumes --filter state=use --filter type=gp2\n  awless-ro list volumes --tag-value Purchased\n  awless-ro list vpcs --tag-key Dept --tag-key Internal\n  awless-ro list instances --tag Env=Production,Dept=Marketing\n  awless-ro list instances --filter state=running,type=micro\n  awless-ro list s3objects --filter bucket=pdf-bucket \n  awless-ro list accesskeys --filter state==Active",
	PersistentPreRun:  applyHooks(initLoggerHook, initAwlessEnvHook, initCloudServicesHook, firstInstallDoneHook),
	PersistentPostRun: applyHooks(onVersionUpgrade, networkMonitorHook),
	Short:             "List resources: sorting, filtering via tag/properties, output formatting, etc...",
	Annotations:       map[string]string{formatsAnnotation: "any"},
}

var listSpecificResourceCmd = func(resType string) *cobra.Command {
	return &cobra.Command{
		ValidArgsFunction: cobra.NoFileCompletions,
		Use:               cloud.PluralizeResource(resType),
		Short:             fmt.Sprintf("[%s] List %s %s", awsservices.ServicePerResourceType[resType], strings.ToUpper(awsservices.APIPerResourceType[resType]), cloud.PluralizeResource(resType)),

		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 {
				var plural string
				if len(args) > 1 {
					plural = "s"
				}
				logger.Errorf("invalid parameter%s '%s'", plural, strings.Join(args, " "))
				if strings.Contains(args[0], "=") {
					if !promptConfirmDefaultYes("Did you mean `awless-ro list %s --filter %s`? ", cloud.PluralizeResource(resType), strings.Join(args, " ")) {
						os.Exit(1)
					}
					listingFiltersFlag = append(listingFiltersFlag, args...)
				} else {
					os.Exit(1)
				}
			}
			var g cloud.GraphAPI

			if localGlobalFlag {
				if srvName, ok := awsservices.ServicePerResourceType[resType]; ok {
					warnIfNotSynced(srvName)
					if resType == cloud.S3Object {
						noteObjectsNotKept()
					}
					var err error
					g, err = sync.LoadLocalGraphForService(srvName, config.GetAWSProfile(), config.GetAWSRegion())
					exitOn(err)
				} else {
					exitOn(fmt.Errorf("cannot find service for resource type %s", resType))
				}
			} else {
				srv, err := cloud.GetServiceForType(resType)
				exitOn(err)
				fetchContext := context.WithValue(context.Background(), "force", true)
				g, err = srv.FetchByType(context.WithValue(fetchContext, "filters", listingFiltersFlag), resType)
				exitOn(err)
			}

			printResources(g, resType)
		},
	}
}

var listAllResourceInServiceCmd = func(srvName string) *cobra.Command {
	return &cobra.Command{
		ValidArgsFunction: cobra.NoFileCompletions,
		Use:               srvName,
		Short:             fmt.Sprintf("List all %s resources", srvName),
		Hidden:            true,

		Run: func(cmd *cobra.Command, args []string) {
			warnIfNotSynced(srvName)
			g, err := sync.LoadLocalGraphForService(srvName, config.GetAWSProfile(), config.GetAWSRegion())
			exitOn(err)
			displayer, err := console.BuildOptions(
				console.WithFormat(formatGlobalFlag),
				console.WithMaxWidth(tableMaxWidth()),
				// --columns was accepted and ignored here, so asking a
				// service-wide listing for particular columns silently got the
				// default ones. There is no single resource type to look the
				// columns up for, so each name is read as a property, which is
				// what every resource has in common.
				console.WithColumns(listingColumnsFlag),
				console.WithIDsOnly(listOnlyIDs),
			).SetSource(g).Build()
			exitOn(err)
			exitOn(displayer.Print(os.Stdout))
		},
	}
}

// warnIfNotSynced says so when --local is asked to read a local copy that does not
// exist yet, instead of letting the command answer "No results found." — which says
// something about the account, in a situation where the account was never read. The
// copy is per service and region: a sync of another region leaves the global files
// behind, which are no answer for this region's instances. In JSON the hint is a note,
// a plain stderr line like the inspectors' notes, so that a caller who keeps the logs
// out of the result still gets it.
func warnIfNotSynced(service string) {
	profile, region := config.GetAWSProfile(), config.GetAWSRegion()
	var msg string
	switch {
	case sync.NothingSyncedFor(profile, region):
		msg = fmt.Sprintf("nothing has been synced for profile '%s' in region '%s' yet, so --local has "+
			"nothing to read. Run `awless-ro sync`, or drop --local to ask AWS directly.", profile, region)
	case !sync.ServiceSyncedFor(service, profile, region):
		msg = fmt.Sprintf("'%s' has not been synced for profile '%s' in region '%s' yet, so --local has "+
			"nothing to read for it. Run `awless-ro sync --%s`, or drop --local to ask AWS directly.",
			service, profile, region, service)
	default:
		return
	}
	printNote(msg)
}

// noteObjectsNotKept says why `list s3objects --local` is empty when the buckets are
// not: a sync keeps a count and a size per bucket, not the objects themselves.
func noteObjectsNotKept() {
	v, _ := config.Get("aws.storage.s3object.keep")
	if keep, _ := v.(bool); keep {
		return
	}
	printNote("a sync keeps the number and total size of the objects in each bucket, not the objects: " +
		"see `awless-ro inspect -i bucket_sizer`. Drop --local to list the objects from AWS, or keep " +
		"them from the next sync on with `awless-ro config set aws.storage.s3object.keep true`.")
}

// printNote is logged, except in JSON, where it is a plain stderr line like the
// inspectors' notes, so that a caller who keeps the logs out of the result still
// gets it.
func printNote(msg string) {
	if jsonOutput() {
		fmt.Fprintln(os.Stderr, msg)
		return
	}
	logger.Info(msg)
}

func printResources(g cloud.GraphAPI, resType string) {
	displayer, err := console.BuildOptions(
		console.WithRdfType(resType),
		console.WithColumns(listingColumnsFlag),
		console.WithFilters(listingFiltersFlag),
		console.WithTagFilters(listingTagFiltersFlag),
		console.WithTagKeyFilters(listingTagKeyFiltersFlag),
		console.WithTagValueFilters(listingTagValueFiltersFlag),
		console.WithMaxWidth(tableMaxWidth()),
		console.WithFormat(formatGlobalFlag),
		console.WithIDsOnly(listOnlyIDs),
		console.WithSortBy(sortBy...),
		console.WithReverseSort(reverseFlag),
		console.WithNoHeaders(noHeadersFlag),
	).SetSource(g).Build()
	exitOn(err)

	exitOn(displayer.Print(os.Stdout))
}
