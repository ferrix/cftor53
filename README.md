# CloudFlare to Route53 Subdomain Delegation

This CDK project sets up a Route53 hosted zone for a subdomain of a domain hosted on Cloudflare, allowing you to serve the subdomain from AWS while keeping the parent domain on Cloudflare.

## Architecture

The stack creates:
1. A Route53 public hosted zone for your subdomain
2. A DNS-validated ACM certificate for the subdomain (in us-east-1 for CloudFront compatibility)
3. CloudFormation outputs with the name servers to add to Cloudflare
4. A custom resource that automatically manages NS records in Cloudflare

## Features

- Cross-region resources: Hosted zone in your preferred region and certificate in us-east-1
- Cross-region references properly handled using CDK's cross-region capabilities
- SSM Parameters for referencing resources in your own infrastructure
- Secure storage of Cloudflare API token in AWS Secrets Manager
- Automatic checking for colliding DNS records in Cloudflare
- Automatic updating of NS records in Cloudflare
- Go-based Lambda function for efficient and reliable Cloudflare DNS management

## Prerequisites

- AWS account and configured AWS CLI
- AWS CDK installed (`npm install -g aws-cdk`)
- Go 1.18 or later
- Domain registered and managed in Cloudflare
- Cloudflare API token with Zone:Edit and DNS:Edit permissions

## Deployment Steps

1. Clone this repository

2. Build the Go Lambda function:
   ```bash
   cd lambda
   chmod +x build.sh
   ./build.sh
   cd ..
   ```

3. Update the `main()` function in `cftor53.go` with your domain details:
   ```go
   parentDomain := jsii.String("example.com") // Replace with your Cloudflare-hosted domain
   subdomain := jsii.String("sub")            // Replace with your desired subdomain
   ```

4. Update the account ID and region in the `env()` function:
   ```go
   return &awscdk.Environment{
       Account: jsii.String("123456789012"), // Replace with your AWS account ID
       Region:  jsii.String("eu-north-1"),   // Replace with your preferred region
   }
   ```

5. Create a `config.json` file with your Cloudflare API token:
   ```json
   {
     "api_token": "your-cloudflare-api-token"
   }
   ```

6. Deploy the stack:
   ```bash
   cdk deploy --all
   ```

7. The stack will automatically handle the Cloudflare DNS setup:
   - It will check for any conflicting DNS records
   - It will automatically create the necessary NS records in Cloudflare

## How It Works

The solution uses three separate stacks:

1. **Secret Stack (CfCloudflareSecretsStack)**: Creates an AWS Secrets Manager secret to securely store your Cloudflare API token.

2. **Main Stack (Cftor53Stack)**: Creates the Route53 hosted zone for your subdomain in your preferred region and implements a custom resource that:
   - Checks for any colliding DNS records in Cloudflare
   - Automatically creates NS records in Cloudflare pointing to the Route53 nameservers
   - Updates the NS records if they change

3. **Certificate Stack (Cftor53CertificateStack)**: Creates an ACM certificate in the us-east-1 region (required for CloudFront compatibility).

The stacks use cross-region references to properly link resources between different regions.

## Cloudflare Integration Details

The stack automatically handles Cloudflare DNS setup by:

1. **Security**: Storing the Cloudflare API token in AWS Secrets Manager
2. **Safety**: Checking for colliding DNS records before creating the NS records
3. **Automation**: Creating and updating NS records in Cloudflare to point to Route53
4. **Efficiency**: Using a Go-based Lambda function for reliable and fast operations

The Go Lambda function:
- Validates there are no conflicting DNS records in Cloudflare
- Compares existing NS records with the desired Route53 nameservers
- Adds missing NS records and removes incorrect ones
- Fails deployment if there are conflicting records, ensuring safety

If there are non-NS records for the subdomain in Cloudflare, the deployment will fail with an error message indicating which records are causing conflicts.

## Using the Certificate

The stack outputs the ARN of the generated certificate, which you can use with:
- CloudFront distributions
- API Gateway custom domains
- Application Load Balancers
- Other AWS services that support custom domains

## Troubleshooting

- **DNS Propagation**: DNS changes can take 24-48 hours to fully propagate worldwide.
- **Certificate Validation**: The certificate validation will automatically complete once the DNS records are properly set up.
- **Multiple Subdomains**: For multiple subdomains, deploy separate stacks with different subdomain parameters.
- **Cloudflare API Token**: Ensure your Cloudflare API token has Zone:Edit and DNS:Edit permissions.
- **Lambda Build Issues**: If you encounter issues building the Lambda function, ensure you have Go 1.18+ installed and try running the build script with verbose output: `GOOS=linux GOARCH=amd64 go build -v -o build/main main.go`

## Security

- This setup enables secure HTTPS for your subdomain using AWS ACM.
- Ensure your AWS IAM permissions follow the principle of least privilege.
- The Cloudflare API token is stored securely in AWS Secrets Manager.
