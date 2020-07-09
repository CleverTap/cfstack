package worker

import (
	"fmt"
	"git.wizrocket.net/infra/cfstack/internal/pkg/aws/cloudformation"
	"git.wizrocket.net/infra/cfstack/internal/pkg/aws/s3"
	"git.wizrocket.net/infra/cfstack/internal/pkg/aws/session"
	"git.wizrocket.net/infra/cfstack/internal/pkg/stack"
	"github.com/Jeffail/gabs"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/fatih/color"
	"github.com/golang/glog"
	"github.com/pkg/errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	MaxWorker = 50
	MinWorker = 1
)

type RegionDiffWorkerJob struct {
	Region          string
	Stacks          []stack.Stack
	Profile         string
	StackDiffWorker int
	Uid             string
	TemplatesRoot   string
	Values          *gabs.Container
	Role            string
}

type RegionDiffWorkerResult struct {
	Region string
	Stacks []stack.Stack
	Err    error
}
type StackDiffJob struct {
	Stack *stack.Stack
}

type stackDiffWorkerJob struct {
	region string
	bucket string
	stack  stack.Stack
}

type stackDiffWorkerResult struct {
	Stack stack.Stack
	Err   error
}

func RegionDiffWorker(id int, wg *sync.WaitGroup, regionWorkerJobs <-chan RegionDiffWorkerJob, regionWorkerResults chan<- *RegionDiffWorkerResult) {
	var errResult error
	for regionWorkerJob := range regionWorkerJobs {
		//glog.Infof("region-worker-%d: starting diff for stacks in in region %s\n", id, regionWorkerJob.Region)
		region := regionWorkerJob.Region
		profile := regionWorkerJob.Profile
		templateRoot := regionWorkerJob.TemplatesRoot
		values := regionWorkerJob.Values
		role := regionWorkerJob.Role

		stacks := regionWorkerJob.Stacks

		out := make([]stack.Stack, 0)

		sess, err := session.NewSession(&session.Opts{
			Profile: profile,
			Region:  region,
		})

		if err != nil {
			regionWorkerResults <- &RegionDiffWorkerResult{
				Region: region,
				Stacks: out,
				Err:    err,
			}
			wg.Done()
		}
		uploader := s3.New(sess)
		deployer := cloudformation.New(sess, values)

		bucket, err := deployer.GetStackResourcePhysicalId("cfstack-Init", "TemplatesS3Bucket")
		if err != nil {
			regionWorkerResults <- &RegionDiffWorkerResult{
				Region: region,
				Stacks: out,
				Err:    err,
			}
			wg.Done()
		}

		stackDiffWorkerJobs := make(chan stackDiffWorkerJob, len(stacks))
		stackDiffWorkerResults := make(chan *stackDiffWorkerResult, len(stacks))

		// start workers
		stackDiffWorkerWaitGroup := sync.WaitGroup{}
		workers := regionWorkerJob.StackDiffWorker

		if len(stacks) < MaxWorker {
			workers = len(stacks)
		}
		for i := 1; i <= workers; i++ {
			go stackDiffWorker(i, &stackDiffWorkerWaitGroup, stackDiffWorkerJobs, stackDiffWorkerResults)
		}

		limiter := time.Tick(50 * time.Millisecond)

		for i, s := range stacks {
			<-limiter
			s.SetRegion(regionWorkerJob.Region)
			s.SetUuid(regionWorkerJob.Uid)
			s.SetBucket(bucket)
			s.SetDeploymentOrder(i)
			s.TemplateRootPath = templateRoot

			s.Changes = &cloudformation.Changes{
				Status:            "",
				StatusReason:      "",
				Resources:         []cloudformation.ChangeResource{},
				StackPolicyChange: false,
			}

			s.Uploader = uploader
			s.Deployer = deployer

			s.RoleArn = role

			stackDiffWorkerJobs <- stackDiffWorkerJob{
				region: region,
				bucket: bucket,
				stack:  s,
			}

			stackDiffWorkerWaitGroup.Add(1)
		}

		close(stackDiffWorkerJobs)
		stackDiffWorkerWaitGroup.Wait()

		for n := 0; n < len(stacks); n++ {
			select {
			case diffWorkerResult := <-stackDiffWorkerResults:
				err := diffWorkerResult.Err
				s := diffWorkerResult.Stack

				if err != nil {
					if strings.Contains(err.Error(), "No updates are to be performed") {
						continue
					}

					s.Changes.Status = stack.DiffFailStatus
					s.Changes.StatusReason = err.Error()

					if aerr, ok := err.(awserr.Error); ok {
						s.Changes.StatusReason = aerr.Message()
						switch aerr.Code() {
						case "ValidationError":
							if strings.Contains(aerr.Message(), "IN_PROGRESS") {
								color.New(color.FgYellow).Fprintf(os.Stdout, "    %s\n", aerr.Message())
								s.Changes.Status = stack.DiffUnknownStatus
								s.Changes.StatusReason = aerr.Message()
							} else {
								s.Changes.StatusReason = fmt.Sprintf("Unhandled AWS ValidationError for stack %s\n%s", s.StackName, aerr.Message())
							}
						default:
						}
					}

					if s.Changes.Status == stack.DiffFailStatus {
						color.New(color.FgRed).Fprintf(os.Stdout, "    diff failed for stack %s : %v\n", s.StackName, s.Changes.StatusReason)
						errResult = fmt.Errorf("diff for few stacks in %s region has failed", region)
					}

					out = append(out, s)
					continue
				}

				if s.Changes.StackPolicyChange || s.Changes.ForceStackUpdate || len(s.Changes.Resources) > 0 {
					s.Changes.Status = stack.DiffSuccessStatus
					out = append(out, s)
				}
			}
		}

		sort.Slice(out, func(i, j int) bool {
			return out[i].DeploymentOrder < out[j].DeploymentOrder
		})

		regionWorkerResults <- &RegionDiffWorkerResult{
			Region: region,
			Stacks: out,
			Err:    errResult,
		}
		//glog.Infof("region-worker-%d: finishing diff for stacks in in region %s\n", id, regionWorkerJob.Region)
		wg.Done()
	}
}

func stackDiffWorker(id int, waitGroup *sync.WaitGroup, stackDiffWorkerJobs <-chan stackDiffWorkerJob, stackDiffWorkerResults chan<- *stackDiffWorkerResult) {
	for diffWorkerJob := range stackDiffWorkerJobs {
		s := diffWorkerJob.stack
		//glog.Infof("worker-%d: fetching diff for stack %s in region %s\n", id, s.StackName, s.Region)

		timeout := time.After(24 * time.Hour)
		ticker := time.Tick(10 * time.Second)

		diffComplete := false

		for diffComplete == false {
			select {
			case <-timeout:
				stackDiffWorkerResults <- &stackDiffWorkerResult{Err: errors.New("too many AWS API calls. Try again later")}
			case <-ticker:
				err := s.Diff()

				if err != nil {
					if aerr, ok := err.(awserr.Error); ok {
						switch aerr.Code() {
						case "Throttling":
							glog.Warningf("AWS rate limit error for stack %s, retrying..", s.StackName)
							continue
						case "ValidationError":
							if strings.Contains(aerr.Message(), "S3 error: Access Denied") {
								glog.Warningf("AWS request error for stack %s, retrying..", s.StackName)
								continue
							}
						case "RequestError":
							glog.Warningf("AWS request error for stack %s -  %s, retrying..", s.StackName, aerr.Message())
							continue
						case "ChangeSetNotFound":
							glog.Warningf("Looks like changeset not found for stack %s, retrying..", s.StackName)
							continue
						default:
							glog.Errorf("unhandled AWS error for stack %s\n%s : %s", s.StackName, aerr.Code(), aerr.Message())
						}
					}
				}

				stackDiffWorkerResults <- &stackDiffWorkerResult{
					Stack: s,
					Err:   err,
				}
				diffComplete = true
			}
		}

		//glog.Infof("worker-%d: Received diff for stack %s in region %s\n", id, s.StackName, s.Region)
		waitGroup.Done()
	}
}
