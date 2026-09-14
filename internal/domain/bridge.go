package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// TargetKind is the kind of AWS resource a bridge target reads.
type TargetKind int

const (
	TargetNone TargetKind = iota
	TargetECS             // an ECS service, ID = cluster/service
	TargetSQS             // an SQS queue, ID = queue name
	TargetRDS             // an RDS instance, ID = DB instance identifier
)

var targetKindNames = map[TargetKind]string{TargetECS: "ecs", TargetSQS: "sqs", TargetRDS: "rds"}

// String returns the directive keyword used in bridge.conf.
func (k TargetKind) String() string { return targetKindNames[k] }

// ParseTargetKind maps a bridge.conf keyword to a TargetKind.
func ParseTargetKind(s string) (TargetKind, bool) {
	for k, name := range targetKindNames {
		if name == s {
			return k, true
		}
	}
	return TargetNone, false
}

// Target is one AWS resource the bridge polls and the stream location it
// exports to: samples land at /<Namespace>/<Service>/metrics/<Prefix>.<name>.dat.
type Target struct {
	Kind      TargetKind
	ID        string
	Namespace string
	Service   string
	Prefix    string
}

var prefixRe = regexp.MustCompile(`^[a-z0-9_]+$`)

// ErrBadTarget is wrapped by every Validate failure.
var ErrBadTarget = errors.New("bad target")

// Validate checks that the target is complete and that its parts fit the
// stream layout: the prefix becomes a metric-name segment, so it is limited
// to [a-z0-9_]; an ECS ID must name both the cluster and the service.
func (t Target) Validate() error {
	switch {
	case t.Kind == TargetNone:
		return fmt.Errorf("%w: missing kind", ErrBadTarget)
	case t.ID == "":
		return fmt.Errorf("%w: missing id", ErrBadTarget)
	case t.Namespace == "" || t.Service == "" || t.Prefix == "":
		return fmt.Errorf("%w: want <namespace>/<service>/<prefix>", ErrBadTarget)
	case !prefixRe.MatchString(t.Prefix):
		return fmt.Errorf("%w: prefix %q must match [a-z0-9_]+", ErrBadTarget, t.Prefix)
	case t.Kind == TargetECS:
		if c, s := t.ECSParts(); c == "" || s == "" {
			return fmt.Errorf("%w: ecs id %q must be <cluster>/<service>", ErrBadTarget, t.ID)
		}
	}
	return nil
}

// MetricName is the stream name for one of the target's samples.
func (t Target) MetricName(name string) string { return t.Prefix + "." + name }

// Path renders the target's location as written in bridge.conf.
func (t Target) Path() string { return t.Namespace + "/" + t.Service + "/" + t.Prefix }

// ECSParts splits an ECS target's ID into cluster and service; either is
// empty when the ID is not of the form cluster/service.
func (t Target) ECSParts() (cluster, service string) {
	c, s, ok := strings.Cut(t.ID, "/")
	if !ok || strings.Contains(s, "/") {
		return "", ""
	}
	return c, s
}
