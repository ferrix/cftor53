# CloudFlare to Route53 Subdomain Delegation

This CDK project sets up a Route53 hosted zone for a subdomain of a domain hosted on Cloudflare, allowing you to serve the subdomain from AWS while keeping the parent domain on Cloudflare.

## Architecture

The stack creates:
1. A Route53 public hosted zone for your subdomain
2. A DNS-validated ACM certificate for the subdomain (in us-east-1 for CloudFront compatibility)
3. CloudFormation outputs with the name servers to add to Cloudflare

## Features

- Cross-region resources: Hosted zone in your preferred region and certificate in us-east-1
- Cross-region references properly handled using CDK's cross-region capabilities
- SSM Parameters for referencing resources in your own infrastructure

## Prerequisites

- AWS account and configured AWS CLI
- AWS CDK installed (`npm install -g aws-cdk`)
- Go 1.18 or later
- Domain registered and managed in Cloudflare

## Deployment Steps

1. Clone this repository
2. Update the `main()` function in `cftor53.go` with your domain details:
   ```go
   parentDomain := jsii.String("example.com") // Replace with your Cloudflare-hosted domain
   subdomain := jsii.String("sub")            // Replace with your desired subdomain
   ```

3. Update the account ID and region in the `env()` function:
   ```go
   return &awscdk.Environment{
       Account: jsii.String("123456789012"), // Replace with your AWS account ID
       Region:  jsii.String("eu-north-1"),   // Replace with your preferred region
   }
   ```

4. Deploy the stack:
   ```bash
   cdk deploy --all
   ```

5. After deployment, note the name servers in the CloudFormation outputs.

6. In Cloudflare DNS settings for your domain, add NS records for your subdomain pointing to the Route53 name servers:
   - Type: NS
   - Name: your-subdomain
   - Content: the name servers from the CloudFormation outputs (add each as a separate record)
   - TTL: Auto

## How It Works

The solution uses two separate stacks:

1. **Main Stack (Cftor53Stack)**: Creates the Route53 hosted zone for your subdomain in your preferred region.

2. **Certificate Stack (Cftor53CertificateStack)**: Creates an ACM certificate in the us-east-1 region (required for CloudFront compatibility).

The stacks use cross-region references to properly link resources between different regions.

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

## Security

- This setup enables secure HTTPS for your subdomain using AWS ACM.
- Ensure your AWS IAM permissions follow the principle of least privilege.
