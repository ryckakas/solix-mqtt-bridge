package solarbank

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/grid-x/modbus"
)

// ErrUnsupportedModel is returned by Connect when the device is not a Solarbank Max AC and Config.AllowAnyModel is
// off; retrying won't help.
var ErrUnsupportedModel = errors.New("unsupported Solarbank model")

// Config holds the connection settings for a Reader.
type Config struct {
	// Addr is the device's Modbus TCP endpoint as host:port.
	Addr string
	// UnitID is the Modbus unit id; the Max AC answers on 1.
	UnitID uint8
	// Timeout bounds dialing and each request.
	Timeout time.Duration
	// AllowAnyModel skips the ModelMaxAC check, for models whose register map is believed to match.
	AllowAnyModel bool
}

// The only request path production code has: FC04 reads. A write can't compile through it.
type inputRegisterReader interface {
	ReadInputRegisters(ctx context.Context, address, quantity uint16) ([]byte, error)
}

type connection interface {
	Connect(ctx context.Context) error
	Close() error
}

// Reader polls a Solarbank over one persistent Modbus TCP connection, one request at a time, using input-register
// reads only. It is not safe for concurrent use.
type Reader struct {
	cfg  Config
	conn connection
	regs inputRegisterReader
	now  func() time.Time
}

// NewReader returns a Reader for cfg; it does not connect until Connect.
func NewReader(cfg Config) *Reader {
	h := modbus.NewTCPClientHandler(cfg.Addr, modbus.WithDialer((&net.Dialer{Timeout: cfg.Timeout}).DialContext))
	h.Timeout = cfg.Timeout
	h.SlaveID = cfg.UnitID
	h.IdleTimeout = -1
	return &Reader{cfg: cfg, conn: h, regs: modbus.NewClient(h), now: time.Now}
}

// Connect opens the connection and reads the device identity. It refuses a model other than ModelMaxAC with
// ErrUnsupportedModel unless Config.AllowAnyModel is set; on any error the connection is closed again.
func (r *Reader) Connect(ctx context.Context) (Identity, error) {
	if err := r.conn.Connect(ctx); err != nil {
		return Identity{}, fmt.Errorf("connect to Solarbank at %s: %w", r.cfg.Addr, err)
	}
	id, err := r.readIdentity(ctx)
	if err == nil && id.Model != ModelMaxAC && !r.cfg.AllowAnyModel {
		err = fmt.Errorf("%w: device reports %q, want %q", ErrUnsupportedModel, id.Model, ModelMaxAC)
	}
	if err != nil {
		return id, errors.Join(err, r.conn.Close())
	}
	return id, nil
}

// Read polls the live and battery registers and decodes them into a Snapshot stamped with the completion time.
func (r *Reader) Read(ctx context.Context) (Snapshot, error) {
	live, err := r.readBlock(ctx, liveBlock)
	if err != nil {
		return Snapshot{}, err
	}
	battery, err := r.readBlock(ctx, batteryBlock)
	if err != nil {
		return Snapshot{}, err
	}
	return decodeSnapshot(live, battery, r.now()), nil
}

// Close closes the connection; a later Connect opens a fresh one.
func (r *Reader) Close() error {
	if err := r.conn.Close(); err != nil {
		return fmt.Errorf("close Solarbank connection: %w", err)
	}
	return nil
}

func (r *Reader) readIdentity(ctx context.Context) (Identity, error) {
	identity, err := r.readBlock(ctx, identityBlock)
	if err != nil {
		return Identity{}, err
	}
	model, err := r.readBlock(ctx, modelBlock)
	if err != nil {
		return Identity{}, err
	}
	return decodeIdentity(identity, model), nil
}

func (r *Reader) readBlock(ctx context.Context, b block) (words, error) {
	raw, err := r.regs.ReadInputRegisters(ctx, b.start, b.count)
	if err != nil {
		return words{}, fmt.Errorf("read input registers %d+%d: %w", b.start, b.count, err)
	}
	return wordsFromBytes(b, raw)
}
