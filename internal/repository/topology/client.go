package topology

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/database"
)

// Client owns access to a single topology-server connection pool. The raw
// database handle intentionally remains private so callers cannot bypass the
// repository boundary.
type Client struct {
	db *sql.DB
}

type Row struct {
	row *sql.Row
}

func (r *Row) Decode(dest ...any) error {
	if r == nil || r.row == nil {
		return errors.New("topology repository row is nil")
	}
	return r.row.Scan(dest...)
}

func Open(host string, port int) (*Client, error) {
	db, err := database.OpenTopology(host, port)
	if err != nil {
		return nil, err
	}
	return &Client{db: db}, nil
}

func OpenContext(ctx context.Context, host string, port int) (*Client, error) {
	db, err := database.OpenTopologyContext(ctx, host, port)
	if err != nil {
		return nil, err
	}
	return &Client{db: db}, nil
}

func OpenDiscovery(host string, port int) (*Client, error) {
	return OpenDiscoveryContext(context.Background(), host, port)
}

func OpenDiscoveryContext(ctx context.Context, host string, port int) (*Client, error) {
	db, err := database.OpenDiscoveryContext(ctx, host, port)
	if err != nil {
		return nil, err
	}
	return &Client{db: db}, nil
}

func (c *Client) CheckConnection(ctx context.Context) error {
	if c == nil || c.db == nil {
		return errors.New("topology repository client is nil")
	}
	return c.db.PingContext(ctx)
}

func (c *Client) Execute(query string, args ...any) error {
	return c.ExecuteContext(context.Background(), query, args...)
}

func (c *Client) ExecuteContext(ctx context.Context, query string, args ...any) error {
	if c == nil || c.db == nil {
		return errors.New("topology repository client is nil")
	}
	_, err := c.db.ExecContext(ctx, query, args...)
	return err
}

func (c *Client) Read(query string, dest ...any) error {
	return c.ReadContext(context.Background(), query, dest...)
}

func (c *Client) ReadContext(ctx context.Context, query string, dest ...any) error {
	if c == nil || c.db == nil {
		return errors.New("topology repository client is nil")
	}
	return c.db.QueryRowContext(ctx, query).Scan(dest...)
}

func (c *Client) ReadRow(query string, args ...any) *Row {
	return c.ReadRowContext(context.Background(), query, args...)
}

func (c *Client) ReadRowContext(ctx context.Context, query string, args ...any) *Row {
	if c == nil || c.db == nil {
		return &Row{}
	}
	return &Row{row: c.db.QueryRowContext(ctx, query, args...)}
}

func (c *Client) ReadArgs(query string, args []any, dest ...any) error {
	if c == nil || c.db == nil {
		return errors.New("topology repository client is nil")
	}
	return c.db.QueryRowContext(context.Background(), query, args...).Scan(dest...)
}

func (c *Client) ReadDynamicRows(query string, onRow func(modeldomain.DynamicRow) error, args ...any) error {
	return c.ReadDynamicRowsContext(context.Background(), query, onRow, args...)
}

func (c *Client) ReadDynamicRowsContext(ctx context.Context, query string, onRow func(modeldomain.DynamicRow) error, args ...any) error {
	if c == nil || c.db == nil {
		return errors.New("topology repository client is nil")
	}
	return database.QueryDynamicRowsContext(ctx, c.db, query, onRow, args...)
}

func (c *Client) ReadResultData(query string, args ...any) (modeldomain.ResultData, error) {
	return c.ReadResultDataContext(context.Background(), query, args...)
}

func (c *Client) ReadResultDataContext(ctx context.Context, query string, args ...any) (modeldomain.ResultData, error) {
	if c == nil || c.db == nil {
		return nil, errors.New("topology repository client is nil")
	}
	return database.QueryResultDataContext(ctx, c.db, query, args...)
}

func (c *Client) CommitProbe(ctx context.Context) (returnErr error) {
	if c == nil || c.db == nil {
		return errors.New("topology repository client is nil")
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	return tx.Commit()
}

// InjectEmptyGTIDTransaction executes the session-scoped GTID sequence on one
// dedicated connection. Keeping the connection and transaction private is
// essential: GTID_NEXT must never leak to another pooled session.
func (c *Client) InjectEmptyGTIDTransaction(ctx context.Context, gtid string) (returnErr error) {
	if c == nil || c.db == nil {
		return errors.New("topology repository client is nil")
	}
	conn, err := c.db.Conn(ctx)
	if err != nil {
		return err
	}
	resetNeeded := false
	defer func() {
		if resetNeeded {
			cleanupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, resetErr := conn.ExecContext(cleanupContext, `SET GTID_NEXT="AUTOMATIC"`)
			cancel()
			returnErr = errors.Join(returnErr, resetErr)
		}
		returnErr = errors.Join(returnErr, conn.Close())
	}()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`SET GTID_NEXT="%s"`, gtid)); err != nil {
		return err
	}
	resetNeeded = true
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `SET GTID_NEXT="AUTOMATIC"`)
	if err == nil {
		resetNeeded = false
	}
	return err
}

func (c *Client) ReadGroupReplicationMembers(ctx context.Context) (members []modeldomain.GroupReplicationMember, rowErrors []error, supported bool, returnErr error) {
	if c == nil || c.db == nil {
		return nil, nil, false, errors.New("topology repository client is nil")
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT MEMBER_ID, MEMBER_HOST, MEMBER_PORT, MEMBER_STATE, MEMBER_ROLE,
			@@global.group_replication_group_name,
			@@global.group_replication_single_primary_mode
		FROM performance_schema.replication_group_members
		WHERE MEMBER_STATE != 'OFFLINE'
	`)
	if err != nil {
		if mysqlError, ok := errors.AsType[*mysql.MySQLError](err); ok {
			switch mysqlError.Number {
			case 1146, 1193:
				return nil, nil, false, nil
			}
		}
		return nil, nil, true, err
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	for rows.Next() {
		var member modeldomain.GroupReplicationMember
		if err := rows.Scan(&member.UUID, &member.Host, &member.Port, &member.State, &member.Role, &member.GroupName, &member.SinglePrimaryGroup); err != nil {
			rowErrors = append(rowErrors, err)
			continue
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return members, rowErrors, true, err
	}
	return members, rowErrors, true, nil
}
