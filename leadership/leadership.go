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
	"github.com/spf13/viper"

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
}

// Member represents a leadership eligible cluster node
type Member struct {
	Id           string
	Registration int64
	Heartbeat    int64
}

// NewLeaderElection creates an instance of a LeaderElection struct
func NewLeaderElection() *LeaderElection {
	if leaderElectionInstance == nil {
		leaderElectionLock.Lock()
		defer leaderElectionLock.Unlock()
		if leaderElectionInstance == nil {
			s := storage.StorageAdapterFactory{}
			storageAdapter, err := s.GetInstance(storage.DEFAULT)
			if err != nil {
				logger.Fatal("failed to create LeaderElection instance", slog.Any("error", err.Error()))
			}
			heartbeatInterval := viper.GetDuration("leadership.heartbeat")
			if heartbeatInterval == 0 {
				heartbeatInterval = DEFAULT_HEARTBEAT
			}
			leaderElectionInstance = &LeaderElection{
				Id:                uuid.NewString(),
				Results:           make(chan string),
				storage:           storageAdapter,
				storageType:       viper.GetString("storage.type"),
				storageProvider:   viper.GetString("storage.provider"),
				heartbeatInterval: heartbeatInterval,
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
			statement = "CREATE TABLE IF NOT EXISTS members (id TEXT PRIMARY KEY, registration NUMERIC, heartbeat NUMERIC)"
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
		a := storage.GetDynamoDBAdapterInstance()
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
				global := viper.GetBool("storage.config.global")
				if global {
					region := viper.GetString("storage.config.region")
					regions := viper.GetStringSlice("storage.config.regions")
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
	var statement string

	switch l.storageType {
	case string(storage.SQL):
		statement = fmt.Sprintf(`INSERT INTO members VALUES('%v', %v, %v)`, l.Id, now, now)
	case string(storage.DYNAMODB):
		statement = fmt.Sprintf(`INSERT INTO members VALUE {'id': '%v', 'registration': %v, 'heartbeat': %v}`, l.Id, now, now)
	}

	return l.storage.Execute(statement)
}

// removeMember removes a cluster node from the database table used for leader election
func (l *LeaderElection) removeMember(memberId string) error {
	statement := fmt.Sprintf(`DELETE FROM members WHERE id='%v'`, memberId)
	return l.storage.Execute(statement)
}

// heartbeat is used by cluster members to indicate they are still alive
func (l *LeaderElection) heartbeat() {
	for {
		time.Sleep(l.heartbeatInterval)
		now := time.Now().UnixMilli()
		slog.Info("updating heartbeat", slog.Int64("heartbeat", now))
		statement := fmt.Sprintf(`UPDATE members SET heartbeat=%v WHERE id='%s'`, now, l.Id)
		err := l.storage.Execute(statement)
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
		statement := fmt.Sprintf(`SELECT * FROM members WHERE id='%s'`, l.Leader.Id)
		a := storage.GetSQLAdapterInstance()
		result := a.DB.Raw(statement).Scan(&member)
		if result.Error != nil {
			err = fmt.Errorf("failed to get leader: %v", result.Error)
		}
	case string(storage.DYNAMODB):
		key, marshalErr := attributevalue.MarshalMap(map[string]string{"id": l.Leader.Id})
		if marshalErr != nil {
			err = fmt.Errorf("failed to get leader: %v", marshalErr)
		} else {
			a := storage.GetDynamoDBAdapterInstance()
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
	statement := "SELECT * FROM members"
	switch l.storageType {
	case string(storage.SQL):
		a := storage.GetSQLAdapterInstance()
		result := a.DB.Raw(statement).Scan(&members)
		if result.Error != nil {
			err = fmt.Errorf("failed to list cluster members: %v", result.Error)
		}
	case string(storage.DYNAMODB):
		statement := "SELECT * FROM members"
		a := storage.GetDynamoDBAdapterInstance()
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
