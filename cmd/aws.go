package cmd

import "fmt"

const assumeRolePolicy = `{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "%s",
		"Principal": {"AWS": "%s"},
		"Action": "sts:AssumeRole"
	}]
}`

const adminPolicy = `{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "Allow",
		"Action": "*",
		"Resource": "*"
	}]
}`

func newAssumeRolePolicy(principal string) string {
	if principal == "" {
		return fmt.Sprintf(assumeRolePolicy, "Deny", "*")
	}
	if isAWSAccountID(principal) {
		return fmt.Sprintf(assumeRolePolicy, "Allow",
			"arn:aws:iam::"+principal+":root")
	}
	return fmt.Sprintf(assumeRolePolicy, "Allow", principal)
}
