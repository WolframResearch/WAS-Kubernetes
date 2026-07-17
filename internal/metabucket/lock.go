package metabucket

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// LockTableName returns the companion DynamoDB table name for a cluster and AWS account.
func LockTableName(clusterName, accountID string) string {
	if clusterName == "" {
		clusterName = "wasctl"
	}
	return "wolfram-" + clusterName + "-meta-lock-" + accountID
}

// Lock manages a per-cluster advisory lock in DynamoDB.
type Lock struct {
	client    *dynamodb.Client
	tableName string
	lockID    string // "clusters/<cluster-name>"
	holder    string // "<hostname>-<pid>-<unix-ts>"
}

// EnsureLockTable creates the DynamoDB lock table if it doesn't exist.
// The table uses PAY_PER_REQUEST billing and a string hash key "LockID",
// matching the terraform remote state locking convention.
func EnsureLockTable(ctx context.Context, region, accountID, clusterName string) error {
	client, err := newDDBClient(ctx, region)
	if err != nil {
		return err
	}
	tableName := LockTableName(clusterName, accountID)

	_, descErr := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	})
	if descErr == nil {
		return nil // table already exists
	}
	var notFound *dbtypes.ResourceNotFoundException
	if !errors.As(descErr, &notFound) {
		return fmt.Errorf("describe lock table: %w", descErr)
	}

	_, err = client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   aws.String(tableName),
		BillingMode: dbtypes.BillingModePayPerRequest,
		AttributeDefinitions: []dbtypes.AttributeDefinition{{
			AttributeName: aws.String("LockID"),
			AttributeType: dbtypes.ScalarAttributeTypeS,
		}},
		KeySchema: []dbtypes.KeySchemaElement{{
			AttributeName: aws.String("LockID"),
			KeyType:       dbtypes.KeyTypeHash,
		}},
		Tags: []dbtypes.Tag{
			{Key: aws.String("ManagedBy"), Value: aws.String("wasctl")},
		},
	})
	if err != nil {
		var exists *dbtypes.ResourceInUseException
		if errors.As(err, &exists) {
			return nil // concurrent creation, fine
		}
		return fmt.Errorf("create lock table: %w", err)
	}

	// Wait for the table to become active before returning.
	waiter := dynamodb.NewTableExistsWaiter(client)
	return waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(tableName)},
		30*time.Second)
}

// NewLock returns a Lock for the given cluster. Call EnsureLockTable first.
func NewLock(ctx context.Context, region, accountID, clusterName string) (*Lock, error) {
	client, err := newDDBClient(ctx, region)
	if err != nil {
		return nil, err
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	holder := fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), time.Now().Unix())
	return &Lock{
		client:    client,
		tableName: LockTableName(clusterName, accountID),
		lockID:    "clusters/" + clusterName,
		holder:    holder,
	}, nil
}

// Acquire atomically acquires the lock via a DynamoDB conditional put.
// Fails if the lock is already held by another process.
func (l *Lock) Acquire(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := l.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(l.tableName),
		ConditionExpression: aws.String("attribute_not_exists(LockID)"),
		Item: map[string]dbtypes.AttributeValue{
			"LockID":     &dbtypes.AttributeValueMemberS{Value: l.lockID},
			"Holder":     &dbtypes.AttributeValueMemberS{Value: l.holder},
			"AcquiredAt": &dbtypes.AttributeValueMemberS{Value: now},
		},
	})
	if err != nil {
		var condFailed *dbtypes.ConditionalCheckFailedException
		if errors.As(err, &condFailed) {
			holder, acquiredAt := l.currentHolder(ctx)
			return fmt.Errorf(
				"another wasctl process is operating on cluster %q (lock held by %s since %s).\n"+
					"Wait for it to finish, or if that process died, run: wasctl unlock %s",
				l.clusterName(), holder, acquiredAt, l.clusterName(),
			)
		}
		return fmt.Errorf("acquire lock: %w", err)
	}
	return nil
}

// Release releases the lock. It only deletes the item if the holder matches
// (so a stale release from a dead process doesn't steal a live lock).
// Idempotent: returns nil if the lock is already gone.
func (l *Lock) Release(ctx context.Context) error {
	_, err := l.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:           aws.String(l.tableName),
		ConditionExpression: aws.String("Holder = :h"),
		ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
			":h": &dbtypes.AttributeValueMemberS{Value: l.holder},
		},
		Key: map[string]dbtypes.AttributeValue{
			"LockID": &dbtypes.AttributeValueMemberS{Value: l.lockID},
		},
	})
	if err != nil {
		var condFailed *dbtypes.ConditionalCheckFailedException
		if errors.As(err, &condFailed) {
			return nil // lock no longer held by us — already gone or stolen after we died
		}
		return fmt.Errorf("release lock: %w", err)
	}
	return nil
}

// ForceRelease deletes the lock unconditionally. Used by `wasctl unlock`.
func (l *Lock) ForceRelease(ctx context.Context) error {
	_, err := l.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(l.tableName),
		Key: map[string]dbtypes.AttributeValue{
			"LockID": &dbtypes.AttributeValueMemberS{Value: l.lockID},
		},
	})
	if err != nil {
		return fmt.Errorf("force release lock: %w", err)
	}
	return nil
}

// currentHolder reads the lock item and returns (holder, acquiredAt) strings
// for use in the lock-conflict error message.
func (l *Lock) currentHolder(ctx context.Context) (holder, acquiredAt string) {
	out, err := l.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(l.tableName),
		Key: map[string]dbtypes.AttributeValue{
			"LockID": &dbtypes.AttributeValueMemberS{Value: l.lockID},
		},
	})
	if err != nil || out.Item == nil {
		return "unknown", "unknown"
	}
	if v, ok := out.Item["Holder"].(*dbtypes.AttributeValueMemberS); ok {
		holder = v.Value
	}
	if v, ok := out.Item["AcquiredAt"].(*dbtypes.AttributeValueMemberS); ok {
		acquiredAt = v.Value
	}
	return
}

func (l *Lock) clusterName() string {
	// lockID is "clusters/<name>"
	if len(l.lockID) > len("clusters/") {
		return l.lockID[len("clusters/"):]
	}
	return l.lockID
}

// newDDBClient builds a DynamoDB client using default credential chain.
func newDDBClient(ctx context.Context, region string) (*dynamodb.Client, error) {
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return dynamodb.NewFromConfig(cfg), nil
}

// unused suppresses "imported and not used" for strconv in non-test builds.
var _ = strconv.Itoa

// DestroyLockTable deletes the DynamoDB lock table for a cluster.
func DestroyLockTable(ctx context.Context, region, accountID, clusterName string) error {
	client, err := newDDBClient(ctx, region)
	if err != nil {
		return err
	}
	tableName := LockTableName(clusterName, accountID)
	_, err = client.DeleteTable(ctx, &dynamodb.DeleteTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		var notFound *dbtypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil // already gone
		}
		return fmt.Errorf("delete DynamoDB table %s: %w", tableName, err)
	}
	return nil
}
