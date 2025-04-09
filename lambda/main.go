package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/cloudflare/cloudflare-go"
)

// CloudflareSecret represents the structure of the secret stored in AWS Secrets Manager
type CloudflareSecret struct {
	ApiToken string `json:"api_token"`
}

// CloudFormationEvent represents an event sent by CloudFormation when a custom resource is provisioned
type CloudFormationEvent struct {
	RequestType           string                  `json:"RequestType"`
	ResponseURL           string                  `json:"ResponseURL"`
	StackId               string                  `json:"StackId"`
	RequestId             string                  `json:"RequestId"`
	ResourceType          string                  `json:"ResourceType"`
	LogicalResourceId     string                  `json:"LogicalResourceId"`
	ResourceProperties    CloudflareDNSProperties `json:"ResourceProperties"`
	PhysicalResourceId    string                  `json:"PhysicalResourceId,omitempty"`
	OldResourceProperties map[string]interface{}  `json:"OldResourceProperties,omitempty"`
}

// CloudflareDNSEvent defines the input event structure for the Lambda function
type CloudflareDNSEvent struct {
	RequestType string                  `json:"RequestType"`
	Properties  CloudflareDNSProperties `json:"ResourceProperties"`
}

// CloudflareDNSProperties defines the properties passed to the Lambda function
type CloudflareDNSProperties struct {
	SecretID    string   `json:"SecretId"`
	Domain      string   `json:"Domain"`
	Subdomain   string   `json:"Subdomain"`
	NameServers []string `json:"NameServers,omitempty"`
	Action      string   `json:"Action"` // "check" or "update"
}

// CloudflareDNSResult represents the result of the Lambda function execution
type CloudflareDNSResult struct {
	StatusCode int    `json:"statusCode"`
	Body       string `json:"body"`
}

// CloudFormationResponse represents the response to send back to CloudFormation
type CloudFormationResponse struct {
	Status             string                 `json:"Status"`
	Reason             string                 `json:"Reason,omitempty"`
	PhysicalResourceId string                 `json:"PhysicalResourceId"`
	StackId            string                 `json:"StackId"`
	RequestId          string                 `json:"RequestId"`
	LogicalResourceId  string                 `json:"LogicalResourceId"`
	Data               map[string]interface{} `json:"Data,omitempty"`
}

// ResponseBody represents the body of the Lambda function response
type ResponseBody struct {
	Domain             string   `json:"domain"`
	Subdomain          string   `json:"subdomain"`
	ZoneID             string   `json:"zoneId"`
	NSRecordsDeleted   int      `json:"nsRecordsDeleted"`
	NSRecordsAdded     int      `json:"nsRecordsAdded"`
	Route53NameServers []string `json:"route53NameServers"`
}

// getSecret retrieves a secret from AWS Secrets Manager
func getSecret(secretID string) (*CloudflareSecret, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %v", err)
	}

	svc := secretsmanager.New(sess)
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	}

	result, err := svc.GetSecretValue(input)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret value: %v", err)
	}

	var secret CloudflareSecret
	if err := json.Unmarshal([]byte(*result.SecretString), &secret); err != nil {
		return nil, fmt.Errorf("failed to unmarshal secret: %v", err)
	}

	return &secret, nil
}

// sendResponse sends a response back to CloudFormation
func sendResponse(event CloudFormationEvent, status string, reason string, data map[string]interface{}) error {
	physicalResourceId := event.PhysicalResourceId
	if physicalResourceId == "" {
		physicalResourceId = fmt.Sprintf("%s-cloudflare-dns", event.LogicalResourceId)
	}

	responseBody := &CloudFormationResponse{
		Status:             status,
		Reason:             reason,
		PhysicalResourceId: physicalResourceId,
		StackId:            event.StackId,
		RequestId:          event.RequestId,
		LogicalResourceId:  event.LogicalResourceId,
		Data:               data,
	}

	responseJSON, err := json.Marshal(responseBody)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %v", err)
	}

	req, err := http.NewRequest("PUT", event.ResponseURL, bytes.NewBuffer(responseJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send response: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("error sending response. Status: %s", resp.Status)
	}

	return nil
}

// HandleRequest is the main Lambda handler function
func HandleRequest(ctx context.Context, event CloudFormationEvent) error {
	// Log the request type
	log.Println("Received request type:", event.RequestType)

	// For Delete operation, simply return a success response
	if event.RequestType == "Delete" {
		return sendResponse(event, "SUCCESS", "Resource deleted", nil)
	}

	// For Create and Update operations, proceed based on the action type
	if event.RequestType == "Create" || event.RequestType == "Update" {
		switch event.ResourceProperties.Action {
		case "check":
			// Only check for collisions, don't update records
			return handleDNSCheck(ctx, event)
		case "update":
			// Update NS records
			return handleDNSUpdate(ctx, event)
		default:
			return sendResponse(event, "FAILED", fmt.Sprintf("Invalid action: %s", event.ResourceProperties.Action), nil)
		}
	}

	return sendResponse(event, "FAILED", fmt.Sprintf("Invalid request type: %s", event.RequestType), nil)
}

// handleDNSCheck checks for colliding DNS records in Cloudflare but doesn't make any changes
func handleDNSCheck(ctx context.Context, event CloudFormationEvent) error {
	props := event.ResourceProperties
	log.Println("Starting Cloudflare DNS collision check")

	// Validate required parameters
	if props.SecretID == "" || props.Domain == "" || props.Subdomain == "" {
		return sendResponse(event, "FAILED", "Missing required parameters", nil)
	}

	// Get Cloudflare API token from Secrets Manager
	secret, err := getSecret(props.SecretID)
	if err != nil {
		return sendResponse(event, "FAILED", fmt.Sprintf("Failed to get secret: %v", err), nil)
	}

	if secret.ApiToken == "" {
		return sendResponse(event, "FAILED", "API token not found in secret", nil)
	}

	// Initialize Cloudflare API client
	api, err := cloudflare.NewWithAPIToken(secret.ApiToken)
	if err != nil {
		return sendResponse(event, "FAILED", fmt.Sprintf("Failed to initialize Cloudflare API client: %v", err), nil)
	}

	// Get the zone ID for the domain
	zoneID, err := api.ZoneIDByName(props.Domain)
	if err != nil {
		return sendResponse(event, "FAILED", fmt.Sprintf("Failed to get zone ID for %s: %v", props.Domain, err), nil)
	}
	log.Println("Found zone ID:", zoneID, "for domain", props.Domain)

	// Create a ResourceContainer for the zone
	rc := cloudflare.ZoneIdentifier(zoneID)

	// Get existing DNS records for the subdomain
	fullDomainName := fmt.Sprintf("%s.%s", props.Subdomain, props.Domain)
	listParams := cloudflare.ListDNSRecordsParams{
		Name: fullDomainName,
	}

	records, _, err := api.ListDNSRecords(ctx, rc, listParams)
	if err != nil {
		return sendResponse(event, "FAILED", fmt.Sprintf("Failed to check DNS records: %v", err), nil)
	}

	// Check for colliding records (non-NS records)
	var collidingRecords []cloudflare.DNSRecord
	for _, record := range records {
		if record.Type != "NS" {
			collidingRecords = append(collidingRecords, record)
		}
	}

	if len(collidingRecords) > 0 {
		var recordTypes []string
		for _, record := range collidingRecords {
			recordTypes = append(recordTypes, record.Type)
		}
		return sendResponse(event, "FAILED", fmt.Sprintf("Found colliding DNS records for %s: %v. Please remove these records first", fullDomainName, recordTypes), nil)
	}

	// Create response data
	data := map[string]interface{}{
		"Domain":    props.Domain,
		"Subdomain": props.Subdomain,
		"ZoneID":    zoneID,
		"Message":   "No colliding DNS records found",
	}

	return sendResponse(event, "SUCCESS", "DNS collision check completed successfully", data)
}

// handleDNSUpdate updates NS records in Cloudflare for the subdomain
func handleDNSUpdate(ctx context.Context, event CloudFormationEvent) error {
	props := event.ResourceProperties
	log.Println("Starting Cloudflare NS record update")

	// Validate required parameters
	if props.SecretID == "" || props.Domain == "" || props.Subdomain == "" || len(props.NameServers) == 0 {
		return sendResponse(event, "FAILED", "Missing required parameters", nil)
	}

	// Get Cloudflare API token from Secrets Manager
	secret, err := getSecret(props.SecretID)
	if err != nil {
		return sendResponse(event, "FAILED", fmt.Sprintf("Failed to get secret: %v", err), nil)
	}

	if secret.ApiToken == "" {
		return sendResponse(event, "FAILED", "API token not found in secret", nil)
	}

	// Initialize Cloudflare API client
	api, err := cloudflare.NewWithAPIToken(secret.ApiToken)
	if err != nil {
		return sendResponse(event, "FAILED", fmt.Sprintf("Failed to initialize Cloudflare API client: %v", err), nil)
	}

	// Get the zone ID for the domain
	zoneID, err := api.ZoneIDByName(props.Domain)
	if err != nil {
		return sendResponse(event, "FAILED", fmt.Sprintf("Failed to get zone ID for %s: %v", props.Domain, err), nil)
	}
	log.Println("Found zone ID:", zoneID, "for domain", props.Domain)

	// Create a ResourceContainer for the zone
	rc := cloudflare.ZoneIdentifier(zoneID)

	// Get existing DNS records for the subdomain
	fullDomainName := fmt.Sprintf("%s.%s", props.Subdomain, props.Domain)
	listParams := cloudflare.ListDNSRecordsParams{
		Name: fullDomainName,
	}

	records, _, err := api.ListDNSRecords(ctx, rc, listParams)
	if err != nil {
		return sendResponse(event, "FAILED", fmt.Sprintf("Failed to check DNS records: %v", err), nil)
	}

	// Get existing NS records
	var existingNSRecords []cloudflare.DNSRecord
	for _, record := range records {
		if record.Type == "NS" {
			existingNSRecords = append(existingNSRecords, record)
		}
	}
	log.Println("Found", len(existingNSRecords), "existing NS records for", fullDomainName)

	// Extract existing nameservers (removing trailing dots)
	var existingNameservers []string
	for _, record := range existingNSRecords {
		existingNameservers = append(existingNameservers, strings.TrimSuffix(record.Content, "."))
	}

	// Remove trailing dots from Route53 nameservers
	var route53NameServersClean []string
	for _, ns := range props.NameServers {
		route53NameServersClean = append(route53NameServersClean, strings.TrimSuffix(ns, "."))
	}

	// Identify nameservers to add and remove
	var nsToAdd []string
	var nsRecordsToRemove []cloudflare.DNSRecord

	for _, ns := range route53NameServersClean {
		found := false
		for _, existingNS := range existingNameservers {
			if ns == existingNS {
				found = true
				break
			}
		}
		if !found {
			nsToAdd = append(nsToAdd, ns)
		}
	}

	for _, record := range existingNSRecords {
		found := false
		cleanContent := strings.TrimSuffix(record.Content, ".")
		for _, ns := range route53NameServersClean {
			if cleanContent == ns {
				found = true
				break
			}
		}
		if !found {
			nsRecordsToRemove = append(nsRecordsToRemove, record)
		}
	}

	// Delete incorrect NS records
	deletedCount := 0
	deleteErrors := []string{}
	for _, record := range nsRecordsToRemove {
		err := api.DeleteDNSRecord(ctx, rc, record.ID)
		if err != nil {
			errMsg := fmt.Sprintf("Error deleting NS record %s: %v", record.Content, err)
			log.Println(errMsg)
			deleteErrors = append(deleteErrors, errMsg)
			continue
		}
		log.Println("Deleted NS record", record.Content)
		deletedCount++
	}

	// Add missing NS records
	addedCount := 0
	addErrors := []string{}
	for _, ns := range nsToAdd {
		createParams := cloudflare.CreateDNSRecordParams{
			Type:    "NS",
			Name:    fullDomainName,
			Content: ns,
			TTL:     3600,
		}

		_, err := api.CreateDNSRecord(ctx, rc, createParams)
		if err != nil {
			errMsg := fmt.Sprintf("Error creating NS record for %s: %v", ns, err)
			log.Println(errMsg)
			addErrors = append(addErrors, errMsg)
			continue
		}
		log.Println("Created NS record for", ns)
		addedCount++
	}

	// Create response data
	data := map[string]interface{}{
		"Domain":             props.Domain,
		"Subdomain":          props.Subdomain,
		"ZoneID":             zoneID,
		"NSRecordsDeleted":   deletedCount,
		"NSRecordsAdded":     addedCount,
		"Route53NameServers": route53NameServersClean,
	}

	// Add error information if there were any errors
	if len(deleteErrors) > 0 || len(addErrors) > 0 {
		data["Warnings"] = map[string]interface{}{
			"DeleteErrors": deleteErrors,
			"AddErrors":    addErrors,
		}

		// Log warnings prominently
		log.Println("WARNING: There were", len(deleteErrors), "delete errors and", len(addErrors), "add errors during NS record update")
	}

	// If no records were successfully added when they needed to be, consider that a failure
	if len(nsToAdd) > 0 && addedCount == 0 {
		return sendResponse(event, "FAILED",
			fmt.Sprintf("Failed to add any of the %d required NS records. See CloudWatch logs for details.", len(nsToAdd)),
			data)
	}

	// If no records were successfully deleted when they needed to be, add a warning but don't fail
	if len(nsRecordsToRemove) > 0 && deletedCount == 0 {
		log.Println("WARNING: Failed to delete any of the outdated NS records")
	}

	return sendResponse(event, "SUCCESS", "NS records updated successfully", data)
}

func main() {
	lambda.Start(HandleRequest)
}
