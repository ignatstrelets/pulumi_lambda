package main

import (
	"encoding/json"

	"github.com/pulumi/pulumi-aws-apigateway/sdk/v2/go/apigateway"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		policy, err := json.Marshal(map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				map[string]interface{}{
					"Action": "sts:AssumeRole",
					"Effect": "Allow",
					"Principal": map[string]interface{}{
						"Service": "lambda.amazonaws.com",
					},
				},
			},
		})
		if err != nil {
			return err
		}

		role, err := iam.NewRole(ctx, "role", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(policy),
			ManagedPolicyArns: pulumi.StringArray{
				iam.ManagedPolicyAWSLambdaBasicExecutionRole,
				customVpcPolicy,
				customSecretsManagerPolicy,
			},
		})
		if err != nil {
			return err
		}

                lambdaSg, err := ec2.NewSecurityGroup(ctx, "lambdaSecurityGroup", &ec2.SecurityGroupArgs{
                        VpcId: customVpcId,
                        Ingress: ec2.SecurityGroupIngressArray{
                                ec2.SecurityGroupIngressArgs{
                                        Protocol:    pulumi.String("tcp"),
                                        FromPort:    pulumi.Int(80),
                                        ToPort:      pulumi.Int(80),
                                        CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
                                        Description: pulumi.String("Allow HTTP inbound from anywhere for Lambda"),
                                },
                        },
                        Egress: ec2.SecurityGroupEgressArray{
                                ec2.SecurityGroupEgressArgs{
                                        Protocol:   pulumi.String("-1"),
                                        FromPort:   pulumi.Int(0),
                                        ToPort:     pulumi.Int(0),
                                        SecurityGroups: pulumi.StringArray{defaultSgId},
                                        Description: pulumi.String("Allow all outbound traffic to VPC"),
                                },
                        },
                })

		fn, err := lambda.NewFunction(ctx, "fn", &lambda.FunctionArgs{
			Runtime: pulumi.String("dotnet6"),
			Handler: pulumi.String("handler.handler"),
			Role:    role.Arn,
			Code:    pulumi.NewFileArchive("./LambdaDotnet.zip"),
			VpcConfig: &lambda.FunctionVpcConfigArgs{
				SubnetIds:        pulumi.StringArray(subnetPublicA, subnetPrivateA, subnetPrivateB),
				SecurityGroupIds: pulumi.StringArray{pulumi.String(lambdaSg.ID())},
			},
			Environment: &lambda.FunctionEnvironmentArgs{
				Variables: pulumi.StringMap{
					"SECRET_NAME": secretName,
				},
			},
		})
		if err != nil {
			return err
		}

		localPath := "www"
		method := apigateway.MethodGET
		api, err := apigateway.NewRestAPI(ctx, "api", &apigateway.RestAPIArgs{
			Routes: []apigateway.RouteArgs{
				apigateway.RouteArgs{Path: "/", LocalPath: &localPath},
			},
		})
		if err != nil {
			return err
		}

		rdsSg, err := ec2.NewSecurityGroup(ctx, "rdsSecurityGroup", &ec2.SecurityGroupArgs{
			VpcId: customVpcId,
			Ingress: ec2.SecurityGroupIngressArray{
				ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(5432),
					ToPort:      pulumi.Int(5432),
					SecurityGroups: pulumi.StringArray{lambdaSg.ID()},
					Description: pulumi.String("Allow PostgreSQL inbound from Lambda SG"),
				},
			},
			Egress: ec2.SecurityGroupEgressArray{
				ec2.SecurityGroupEgressArgs{
					Protocol:   pulumi.String("tcp"), 
					FromPort:   pulumi.Int(5432),
					ToPort:     pulumi.Int(5432),
					CidrBlocks: pulumi.StringArray{pulumi.String(lambdaSg.ID())},
					Description: pulumi.String("Allow PostgreSQL outbound to Lambda SG"),
				},
			},
		})
                if err != nil {
                        return err
                }

		subnetGroup, err := rds.NewSubnetGroup(ctx, "dbSubnetGroup", &rds.SubnetGroupArgs{
			SubnetIds: pulumi.StringArray(subnetPrivateA, subnetPrivateB),
		})
                if err != nil {
                        return err
                }

		db, err := rds.NewInstance(ctx, "dotnet-psql", &rds.InstanceArgs{
			AllocatedStorage:    pulumi.Int(20),
			StorageType:         pulumi.String("gp2"),
			Engine:              pulumi.String("postgres"),
			EngineVersion:       pulumi.String("14.10"),
			InstanceClass:       pulumi.String("db.t3.micro"),
			Name:                pulumi.String("dotnet-psql"),
			Username:            pulumi.String(dbUsername),
			Password:            pulumi.String(dbPassword), 
			SkipFinalSnapshot:   pulumi.Bool(true),
			DbSubnetGroupName:   subnetGroup.Name,
			VpcSecurityGroupIds: pulumi.StringArray{pulumi.String(rdsSg.ID())},
		})
		if err != nil {
			return err
		}



		ctx.Export("url", api.Url)
		return nil
	})
}
