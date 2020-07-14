package worker

import (
	"fmt"
	"github.com/CleverTap/cfstack/internal/pkg/aws/cloudformation"
	"github.com/CleverTap/cfstack/internal/pkg/aws/s3"
	"github.com/CleverTap/cfstack/internal/pkg/aws/session"
	"github.com/CleverTap/cfstack/internal/pkg/stack"
	"github.com/Jeffail/gabs"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/fatih/color"
	"github.com/golang/glog"
	"github.com/pkg/errors"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	rocket = "🚀"
	check  = "✅"
	cross  = "❌"
)

type RegionDeployWorkerJob struct {
	Region             string
	Stacks             []stack.Stack
	Profile            string
	StackDeployWorkers int
	Uid                string
	TemplatesRoot      string
	Values             *gabs.Container
	Role               string
	ParallelMode       bool
}

type RegionDeployWorkerResult struct {
	Region string
	Err    error
}

type stackDeployWorkerJob struct {
	region       string
	bucket       string
	stack        stack.Stack
	profile      string
	parallelMode bool
}

type stackDeployWorkerResult struct {
	Stack stack.Stack
	Err   error
}

func RegionDeployWorker(id int, wg *sync.WaitGroup, regionWorkerJobs <-chan RegionDeployWorkerJob, regionWorkerResults chan<- *RegionDeployWorkerResult) {
	var errResult error
	for regionWorkerJob := range regionWorkerJobs {
		region := regionWorkerJob.Region
		profile := regionWorkerJob.Profile
		templateRoot := regionWorkerJob.TemplatesRoot
		values := regionWorkerJob.Values
		role := regionWorkerJob.Role
		uid := regionWorkerJob.Uid
		parallelMode := regionWorkerJob.ParallelMode

		stacks := regionWorkerJob.Stacks

		sess, err := session.NewSession(&session.Opts{
			Profile: profile,
			Region:  region,
		})

		if err != nil {
			regionWorkerResults <- &RegionDeployWorkerResult{
				Region: region,
				Err:    err,
			}
			wg.Done()
		}
		uploader := s3.New(sess)
		deployer := cloudformation.New(sess, values)

		bucket, err := deployer.GetStackResourcePhysicalId("cfstack-Init", "TemplatesS3Bucket")
		if err != nil {
			regionWorkerResults <- &RegionDeployWorkerResult{
				Region: region,
				Err:    err,
			}
			wg.Done()
		}

		stackDeployWorkerJobs := make(chan stackDeployWorkerJob, len(stacks))
		stackDeployWorkerResults := make(chan *stackDeployWorkerResult, len(stacks))

		// start workers
		stackDeployWorkerWaitGroup := sync.WaitGroup{}
		workers := regionWorkerJob.StackDeployWorkers

		if len(stacks) < workers {
			workers = len(stacks)
		}
		for i := 1; i <= workers; i++ {
			go stackDeployWorker(i, &stackDeployWorkerWaitGroup, stackDeployWorkerJobs, stackDeployWorkerResults)
		}

		limiter := time.Tick(50 * time.Millisecond)

		for i, s := range stacks {
			<-limiter

			s.SetRegion(region)
			s.SetUuid(uid)
			s.SetBucket(bucket)
			s.SetDeploymentOrder(i)
			s.TemplateRootPath = templateRoot

			s.Uploader = uploader
			s.Deployer = deployer

			s.RoleArn = role
			s.SuppressMessages = parallelMode

			stackDeployWorkerJobs <- stackDeployWorkerJob{
				region:       region,
				bucket:       bucket,
				stack:        s,
				profile:      profile,
				parallelMode: parallelMode,
			}

			stackDeployWorkerWaitGroup.Add(1)
		}

		close(stackDeployWorkerJobs)
		stackDeployWorkerWaitGroup.Wait()

		var errStacks []string

		for n := 0; n < len(stacks); n++ {
			select {
			case deployWorkerResult := <-stackDeployWorkerResults:
				err := deployWorkerResult.Err
				s := deployWorkerResult.Stack

				if err != nil {
					if parallelMode {
						fmt.Printf("==> %s  Deployment completed for stack %s in region %s\n%v\n", cross, s.StackName, s.Region, err)
					} else {
						fmt.Fprintf(os.Stdout, color.RedString("    %v\n", err))
					}
					errStacks = append(errStacks, s.StackName)
				}
			}
		}

		if len(errStacks) > 0 {
			errResult = errors.Errorf("Deployments failed for stack(s) %s in region %s", strings.Join(errStacks, ", "), region)
		}

		regionWorkerResults <- &RegionDeployWorkerResult{
			Region: region,
			Err:    errResult,
		}

		wg.Done()
	}
}

func stackDeployWorker(i int, waitGroup *sync.WaitGroup, stackDeployWorkerJobs chan stackDeployWorkerJob, stackDeployWorkerResults chan *stackDeployWorkerResult) {
	for deployWorkJob := range stackDeployWorkerJobs {
		s := deployWorkJob.stack
		//profile := deployWorkJob.profile
		parallelMode := deployWorkJob.parallelMode

		fmt.Printf("==> %s  Deploying stack %s in region %s\n", rocket, s.StackName, s.Region)

		// TODO remove this section out of cfstack scope
		//stackExists, err := s.Deployer.StackExists(s.StackName)
		//if err != nil {
		//	stackDeployWorkerResults <- &stackDeployWorkerResult{
		//		Stack: s,
		//		Err:   err,
		//	}
		//}
		//if s.DeploymentConfiguration.ScaleUpRequired && stackExists {
		//	sess, err := session.NewSession(&session.Opts{
		//		Profile: profile,
		//		Region:  s.Region,
		//	})
		//
		//	if err != nil {
		//		stackDeployWorkerResults <- &stackDeployWorkerResult{
		//			Stack: s,
		//			Err:   err,
		//		}
		//	}
		//
		//	s.DeploymentConfiguration.Cluster = ecs.New(sess)
		//	s.DeploymentConfiguration.Scaler = asg.New(sess)
		//	err = s.ScaleUpClusterForDeployment()
		//	if err != nil {
		//		stackDeployWorkerResults <- &stackDeployWorkerResult{
		//			Stack: s,
		//			Err:   err,
		//		}
		//	}
		//}

		timeout := time.After(24 * time.Hour)
		ticker := time.Tick(10 * time.Second)

		deployComplete := false

		for deployComplete == false {
			select {
			case <-timeout:
				stackDeployWorkerResults <- &stackDeployWorkerResult{
					Stack: s,
					Err:   errors.New("too many AWS API calls. Try again later"),
				}
			case <-ticker:
				err := s.Deploy()
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

				if parallelMode {
					if err == nil {
						fmt.Printf("==> %s  Deployment completed for stack %s in region %s\n", check, s.StackName, s.Region)
					}
				}

				stackDeployWorkerResults <- &stackDeployWorkerResult{
					Stack: s,
					Err:   err,
				}
				deployComplete = true
			}
		}

		waitGroup.Done()
	}
}
