package leadership

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"

	"github.com/tink3rlabs/magic/logger"
	"github.com/tink3rlabs/magic/storage"
)

var leaderElectionLock = &sync.Mutex{}
var leaderElectionInstance *LeaderElection

const RESULT_ELECTED = "elected"
const DEFAULT_HEARTBEAT = 60 * time.Second

// LeaderElection provides methods for electing a leader out of eligible cluster members
type LeaderElection struct {
	Id                string
	Leader            Member
	Results           chan string
	storageType       string
	storageProvider   string
	storage           storage.StorageAdapter
	heartbeatInterval time.Duration
	props             LeaderElectionProps
}

// Member represents a leadership eligible cluster node
type Member struct {
	Id           string
	Registration int64
	Heartbeat    int64
}

// LeaderElectionProps represents the properties required to instantiate new leader election
type LeaderElectionProps struct {
	HeartbeatInterval time.Duration
	StorageAdapter    storage.StorageAdapter
	AdditionalProps   map[string]any
}

// NewLeaderElection creates an instance of a LeaderElection struct
func NewLeaderElection(props LeaderElectionProps) *LeaderElection {
	if leaderElectionInstance == nil {
		leaderElectionLock.Lock()
		defer leaderElectionLock.Unlock()
		if leaderElectionInstance == nil {
			heartbeatInterval := props.HeartbeatInterval
			if heartbeatInterval == 0 {
				heartbeatInterval = DEFAULT_HEARTBEAT
			}
			leaderElectionInstance = &LeaderElection{
				Id:                uuid.NewString(),
				Results:           make(chan string),
				storage:           props.StorageAdapter,
				storageType:       string(props.StorageAdapter.GetType()),
				storageProvider:   string(props.StorageAdapter.GetProvider()),
				heartbeatInterval: heartbeatInterval,
				props:             props,
			}
		}
	}
	return leaderElectionInstance
}

// createLeadershipTable creates the database table used for leader election
func (l *LeaderElection) createLeadershipTable() error {
	switch l.storageType {

	// SQL Adapter
	case string(storage.SQL):
		var statement string
		switch l.storageProvider {
		case string(storage.POSTGRESQL):
			statement = fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.members (id TEXT PRIMARY KEY, registration NUMERIC, heartbeat NUMERIC)", l.storage.GetSchemaName())
		case string(storage.MYSQL):
			statement = "CREATE TABLE IF NOT EXISTS members (id VARCHAR(50) PRIMARY KEY, registration BIGINT, heartbeat BIGINT)"
		case string(storage.SQLITE):
			statement = "CREATE TABLE IF NOT EXISTS members (id TEXT PRIMARY KEY, registration INTEGER, heartbeat INTEGER)"
		}
		return l.storage.Execute(statement)

	// DynamoDB Adapter
	case string(storage.DYNAMODB):
		input := &dynamodb.CreateTableInput{
			TableName: aws.String("members"),
			AttributeDefinitions: []types.AttributeDefinition{
				{AttributeName: aws.String("id"), AttributeType: types.ScalarAttributeTypeS},
			},
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("id"), KeyType: types.KeyTypeHash},
			},
			ProvisionedThroughput: &types.ProvisionedThroughput{
				ReadCapacityUnits:  aws.Int64(5),
				WriteCapacityUnits: aws.Int64(5),
			},
		}

		// Create table
		a := l.storage.(*storage.DynamoDBAdapter)
		_, err := a.DB.CreateTable(context.TODO(), input)
		tableExistsError := new(types.ResourceInUseException)
		if (err != nil) && (!errors.As(err, &tableExistsError)) {
			return err
		} else {
			waiter := dynamodb.NewTableExistsWaiter(a.DB)
			err = waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{
				TableName: aws.String("members")}, 1*time.Minute)
			if err != nil {
				return err
			} else {
				// check if this needs to be a global table
				global := l.props.AdditionalProps["global"].(bool)
				if global {
					region := l.props.AdditionalProps["region"].(string)
					regions := l.props.AdditionalProps["regions"].([]string)
					replicationGroup := []types.Replica{}

					for _, v := range regions {
						if v != region {
							replicationGroup = append(replicationGroup, types.Replica{RegionName: &v})
						}
					}
					_, err := a.DB.CreateGlobalTable(
						context.TODO(),
						&dynamodb.CreateGlobalTableInput{GlobalTableName: aws.String("membership"), ReplicationGroup: replicationGroup},
					)
					if err != nil {
						return err
					}
				}
				return nil
			}
		}

	default:
		return fmt.Errorf("leader election isn't supported for the %s storage adapter", l.storageType)
	}
}

// updateMembershipTable updates the database table used for leader election
func (l *LeaderElection) updateMembershipTable() error {
	now := time.Now().UnixMilli()

	switch l.storageType {
	case string(storage.SQL):
		member := Member{
			Id:           l.Id,
			Registration: now,
			Heartbeat:    now,
		}
		return l.storage.Create(&member)
	case string(storage.DYNAMODB):
		// Keep the DynamoDB implementation as is for now
		statement := fmt.Sprintf(`INSERT INTO members VALUE {'id': '%v', 'registration': %v, 'heartbeat': %v}`, l.Id, now, now)
		return l.storage.Execute(statement)
	default:
		return fmt.Errorf("unsupported storage type: %s", l.storageType)
	}
}

// removeMember removes a cluster node from the database table used for leader election
func (l *LeaderElection) removeMember(memberId string) error {
	return l.storage.Delete(&Member{}, map[string]any{"id": memberId})
}

// heartbeat is used by cluster members to indicate they are still alive
func (l *LeaderElection) heartbeat() {
	for {
		time.Sleep(l.heartbeatInterval)
		now := time.Now().UnixMilli()
		slog.Debug("updating heartbeat", slog.Int64("heartbeat", now))

		member := Member{
			Id:           l.Id,
			Heartbeat:    now,
			Registration: l.Leader.Registration,
		}

		filter := map[string]any{"id": l.Id}
		err := l.storage.Update(&member, filter)

		if err != nil {
			slog.Error("failed to update heartbeat", slog.Any("error", err))
		}
	}
}

// monitorLeader is a go routine that is used by cluster members to ensure the current leader is still active or trigger a re-election
func (l *LeaderElection) monitorLeader() {
	for {
		time.Sleep(l.heartbeatInterval / 2)
		acceptableInterval := -2 * l.heartbeatInterval

		leader, err := l.getLeader()
		if err != nil {
			slog.Error("error monitoring leader", slog.Any("error", err))
		} else {
			diff := time.Until(time.UnixMilli(leader.Heartbeat))
			if diff >= acceptableInterval {
				slog.Info("leader is healthy", slog.String("leader_id", l.Leader.Id))
			} else {
				slog.Info("Starting re-election due to leader inactivity", slog.String("leader_id", l.Leader.Id), slog.Duration("inactivity_duration", diff))
				err = l.electLeader(true)

				if err != nil {
					slog.Error("failed to elect new leader", slog.Any("error", err))
				}

				if l.Id == l.Leader.Id {
					slog.Info("I am the new leader")
					// Publish election results
					go func() { l.Results <- RESULT_ELECTED }()
					break
				} else {
					slog.Info("detected a change in leadership, new leader is elected and monitoring it", slog.String("leader_id", l.Leader.Id))
				}
			}
		}
	}
}

// electLeader is used to elect a leader from the list of eligible cluster members. It elects the active member with the earliest registration date as leader
func (l *LeaderElection) electLeader(reElection bool) error {
	slog.Info("starting election process")
	leader := l.Leader

	if reElection {
		slog.Info("this is a re-election removing existing leader")
		err := l.removeMember(l.Leader.Id)
		if err != nil {
			return fmt.Errorf("failed to remove leader from membership table: %v", err)
		}
		leader = Member{}
	}

	members, err := l.Members()
	if err != nil {
		return fmt.Errorf("failed to list leader eligible members: %v", err)
	}

	for _, m := range members {
		if leader == (Member{}) {
			// We don't have a leader set pick the current member for now
			leader = m
		}
		if m.Registration <= leader.Registration {
			leader = m
		}
	}
	l.Leader = leader
	return nil
}

// getLeader return the current active leader's record from the database
func (l *LeaderElection) getLeader() (Member, error) {
	var member Member
	var err error
	switch l.storageType {
	case string(storage.SQL):
		a := l.storage.(*storage.SQLAdapter)
		result := a.DB.First(&member, "id = ?", l.Leader.Id)

		if result.Error != nil {
			err = fmt.Errorf("failed to get leader: %v", result.Error)
		}
	case string(storage.DYNAMODB):
		key, marshalErr := attributevalue.MarshalMap(map[string]string{"id": l.Leader.Id})
		if marshalErr != nil {
			err = fmt.Errorf("failed to get leader: %v", marshalErr)
		} else {
			a := l.storage.(*storage.DynamoDBAdapter)
			response, getItemErr := a.DB.GetItem(context.TODO(), &dynamodb.GetItemInput{
				TableName: aws.String("members"),
				Key:       key,
			})

			if getItemErr != nil {
				err = fmt.Errorf("failed to get leader: %v", getItemErr)
			} else {
				err = attributevalue.UnmarshalMap(response.Item, &member)
			}
		}
	}
	return member, err
}

// Members returns a list of cluster members
func (l *LeaderElection) Members() ([]Member, error) {
	var members []Member
	var err error
	switch l.storageType {
	case string(storage.SQL):
		a := l.storage.(*storage.SQLAdapter)
		result := a.DB.Find(&members)
		if result.Error != nil {
			err = fmt.Errorf("failed to list cluster members: %v", result.Error)
		}
	case string(storage.DYNAMODB):
		statement := "SELECT * FROM members"
		a := l.storage.(*storage.DynamoDBAdapter)
		result, execErr := a.DB.ExecuteStatement(context.TODO(), &dynamodb.ExecuteStatementInput{Statement: &statement})
		if execErr != nil {
			err = fmt.Errorf("failed to list cluster members: %v", execErr)
		} else {
			marshalErr := attributevalue.UnmarshalListOfMaps(result.Items, &members)
			if marshalErr != nil {
				err = fmt.Errorf("failed to unmarshal cluster members: %v", marshalErr)
			}
		}
	}
	return members, err
}

// Start triggers a new leader election
func (l *LeaderElection) Start() {
	if l.storageType == string(storage.MEMORY) {
		slog.Info("using memory storage adapter, leader election is only supported with persistent storage")
	} else {
		slog.Info("using a persistent storage adapter, starting leader election")
		slog.Info("creating membership table")
		err := l.createLeadershipTable()
		if err != nil {
			logger.Fatal("failed to create membership table", slog.Any("error", err))
		}
		slog.Info("registering node:", slog.String("node_id", l.Id))
		err = l.updateMembershipTable()
		if err != nil {
			logger.Fatal("failed to register node", slog.Any("error", err))
		}
		go l.heartbeat()
		err = l.electLeader(false)
		if err != nil {
			logger.Fatal("failed to elect leader", slog.Any("error", err))
		}
		if l.Id == l.Leader.Id {
			slog.Info("I was elected leader")
			// Publish election results
			go func() { l.Results <- RESULT_ELECTED }()
		} else {
			slog.Info("monitoring the leader", slog.String("leader_id", l.Leader.Id))
			go l.monitorLeader()
		}
	}
}
