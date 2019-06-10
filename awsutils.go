// Package awsutils provides some helper function for common aws task.
package awsutils

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

//Stack ... Aws Cloud formation stack
type Stack struct {
	Name         string
	TemplateURL  string
	Capabilities []string
	Status       *string
	Region       string
}

//CreateOrUpdate... creates a stack or creates a change set for an existing stack based on given parameters
func (s *Stack) CreateOrUpdate(parameters map[string]string) error {
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(s.Region),
	}))
	cfn := cloudformation.New(sess)

	templateParam, err := s.getTeplateParameters(cfn)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}

	if err := findMissingParametres(templateParam, parameters); err != nil {
		fmt.Println("missing:" + err.Error())
		return err
	}

	cfnParameters := convertToRequiredCfnParameter(templateParam, parameters)
	input := cloudformation.DescribeStacksInput{StackName: &s.Name}
	_, err = cfn.DescribeStacks(&input)

	if err != nil {
		s.createStack(cfn, cfnParameters)
	} else {
		s.createChangeSet(cfn, cfnParameters)
	}
	return err
}
func findMissingParametres(templateParam map[string]*string, parameters map[string]string) error {
	missing := make([]string, 0)
	for key, defaultValue := range templateParam {
		_, doesKeyExist := parameters[key]
		if !doesKeyExist && defaultValue == nil {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("Missing: [%s]", strings.Join(missing, ","))
}
func convertToCfnParameter(parameters map[string]string) []*cloudformation.Parameter {
	result := make([]*cloudformation.Parameter, 0)
	for key, value := range parameters {
		result = append(result, &cloudformation.Parameter{
			ParameterKey:   aws.String(key),
			ParameterValue: aws.String(value),
		})
	}
	return result
}
func convertToRequiredCfnParameter(templateParam map[string]*string, parameters map[string]string) []*cloudformation.Parameter {
	result := make([]*cloudformation.Parameter, 0)
	for key := range templateParam {
		value, ok := parameters[key]
		if ok {
			result = append(result, &cloudformation.Parameter{
				ParameterKey:   aws.String(key),
				ParameterValue: aws.String(value),
			})
		}
	}
	return result
}

//ReadOutputs ...
func (s *Stack) ReadOutputs() (map[string]string, error) {
	parameters := make(map[string]string)
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(s.Region),
	}))
	cfn := cloudformation.New(sess)

	input := cloudformation.DescribeStacksInput{StackName: &s.Name}

	res, err := cfn.DescribeStacks(&input)
	if err != nil {
		return nil, err
	}
	for _, stack := range res.Stacks {
		for _, output := range stack.Outputs {
			parameters[*output.OutputKey] = *output.OutputValue
		}
	}
	return parameters, nil
}

//LoadParameters ...
func LoadParameters(fileName string) (map[string]string, error) {
	parameters := make(map[string]string)

	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		words := strings.Split(scanner.Text(), "=")
		key := words[0]
		value := words[1]
		parameters[key] = value
	}
	return parameters, scanner.Err()
}

//LoadEnvironmentVariables ...
func LoadEnvironmentVariables() (map[string]string, error) {

	parameters := make(map[string]string)
	for _, pair := range os.Environ() {

		keyValues := strings.Split(pair, "=")
		key := keyValues[0]
		value := keyValues[1]
		parameters[key] = value
	}
	return parameters, nil
}

func getAllStacksBy(region string) ([]Stack, error) {
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(region),
	}))
	svc := cloudformation.New(sess)

	var filter = []*string{
		aws.String("CREATE_IN_PROGRESS"),
		aws.String("CREATE_FAILED"),
		aws.String("CREATE_COMPLETE"),
		aws.String("ROLLBACK_IN_PROGRESS"),
		aws.String("ROLLBACK_FAILED"),
		aws.String("ROLLBACK_COMPLETE"),
		aws.String("DELETE_IN_PROGRESS"),
		aws.String("DELETE_FAILED"),
		//aws.String("DELETE_COMPLETE"),
		aws.String("UPDATE_IN_PROGRESS"),
		aws.String("UPDATE_COMPLETE_CLEANUP_IN_PROGRESS"),
		aws.String("UPDATE_COMPLETE"),
		aws.String("UPDATE_ROLLBACK_IN_PROGRESS"),
		aws.String("UPDATE_ROLLBACK_FAILED"),
		aws.String("UPDATE_ROLLBACK_COMPLETE_CLEANUP_IN_PROGRESS"),
		aws.String("UPDATE_ROLLBACK_COMPLETE"),
		aws.String("REVIEW_IN_PROGRESS")}
	input := &cloudformation.ListStacksInput{StackStatusFilter: filter}

	resp, err := svc.ListStacks(input)
	if err != nil {
		fmt.Println("Got error listing stacks:")
		fmt.Println(err.Error())
		return nil, err
	}

	results := make([]Stack, 0)

	for _, summary := range resp.StackSummaries {
		results = append(results, Stack{Name: *summary.StackName, Status: summary.StackStatus, Region: region})
	}
	return results, nil
}

//GetTeplateParameters ...
func (s *Stack) GetTeplateParameters() (map[string]*string, error) {
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(s.Region),
	}))
	cfn := cloudformation.New(sess)

	return s.getTeplateParameters(cfn)
}
func (s *Stack) getTeplateParameters(cfn *cloudformation.CloudFormation) (map[string]*string, error) {

	input := &cloudformation.ValidateTemplateInput{TemplateURL: &s.TemplateURL}
	resp, err := cfn.ValidateTemplate(input)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}
	resultParameters := make(map[string]*string)
	for _, tp := range resp.Parameters {
		resultParameters[*tp.ParameterKey] = tp.DefaultValue
	}
	return resultParameters, nil
}

//CreateStack ...
func (s *Stack) CreateStack(parameters map[string]string) error {

	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(s.Region),
	}))
	cfn := cloudformation.New(sess)
	cfnParameters := convertToCfnParameter(parameters)
	return s.createStack(cfn, cfnParameters)
}
func (s *Stack) createStack(cfn *cloudformation.CloudFormation, parameters []*cloudformation.Parameter) error {

	input := &cloudformation.CreateStackInput{
		TemplateURL:  aws.String(s.TemplateURL),
		StackName:    aws.String(s.Name),
		Capabilities: aws.StringSlice(s.Capabilities),
		Parameters:   parameters}

	_, err := cfn.CreateStack(input)
	if err != nil {
		fmt.Println("Got error creating stack:")
		fmt.Println(err.Error())
		return err
	}

	fmt.Println("Waiting for stack to be created")

	// Wait until stack is created
	desInput := &cloudformation.DescribeStacksInput{StackName: aws.String(s.Name)}
	err = cfn.WaitUntilStackCreateComplete(desInput)
	if err != nil {
		fmt.Println("Got error waiting for stack to be created")
		fmt.Println(err)
		return err
	}

	fmt.Println("Created stack " + s.Name)
	return nil
}

//CreateChangeSet ...
func (s *Stack) CreateChangeSet(parameters map[string]string) error {
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(s.Region),
	}))
	cfn := cloudformation.New(sess)
	cfnParameters := convertToCfnParameter(parameters)
	return s.createChangeSet(cfn, cfnParameters)
}
func (s *Stack) createChangeSet(cfn *cloudformation.CloudFormation, parameters []*cloudformation.Parameter) error {

	t := time.Now()
	changeSetName := s.Name + "-" + t.Format("20060102030405")
	input := &cloudformation.CreateChangeSetInput{
		TemplateURL:   aws.String(s.TemplateURL),
		StackName:     aws.String(s.Name),
		ChangeSetName: aws.String(changeSetName),
		Parameters:    parameters}

	_, err := cfn.CreateChangeSet(input)
	if err != nil {
		fmt.Println("Got error creating change set:")
		fmt.Println(err.Error())
		return err
	}

	fmt.Println("Waiting")

	// Wait until stack is created
	desInput := &cloudformation.DescribeStacksInput{StackName: aws.String(s.Name)}
	err = cfn.WaitUntilStackCreateComplete(desInput)
	if err != nil {
		fmt.Println("Got error waiting for createing a stack change")
		fmt.Println(err)
		return err
	}

	fmt.Println("Created change set " + s.Name)
	return nil
}

//DownloadBucket ...
func DownloadBucket(baseDir, bucket, region, excludePatten string) {
	var wg sync.WaitGroup

	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(region),
	}))

	s3Client := s3.New(sess)

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	}

	result, err := s3Client.ListObjectsV2(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket:
				fmt.Println(s3.ErrCodeNoSuchBucket, aerr.Error())
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			fmt.Println(err.Error())
		}
		return
	}

	for _, s3Obj := range result.Contents {

		matched, err := regexp.Match(excludePatten, []byte(*s3Obj.Key))
		if err != nil || matched {
			continue
		}

		if err = mkDirIfNeeded(baseDir, *s3Obj.Key); err != nil {
			return
		}
		wg.Add(1)
		go saveObject(bucket, baseDir, *s3Obj.Key, sess, &wg)
	}
	wg.Wait()
}
func saveObject(bucket, baseDir, key string, sess *session.Session, wg *sync.WaitGroup) {
	downloader := s3manager.NewDownloader(sess)
	fileName := path.Join(baseDir, key)
	file, err := os.Create(fileName)
	defer wg.Done()
	if err != nil {
		log.Println("Unable to open file" + err.Error())
		return
	}

	defer file.Close()
	_, err = downloader.Download(file, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		log.Println("Unable to download item:" + err.Error())
		return
	}

}
func mkDirIfNeeded(baseDir string, key string) (err error) {
	err = nil
	if lastIdx := strings.LastIndex(key, "/"); lastIdx != -1 {
		prefix := key[:lastIdx]
		dirPath := path.Join(baseDir, prefix)
		if err = os.MkdirAll(dirPath, os.ModePerm); err != nil {
			return
		}
	}
	return
}