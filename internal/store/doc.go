// Package store keeps homing's own state (session read/pinned/muted/archived,
// paired devices, ingest tokens) in the data directory. Writes are atomic
// (temp file + rename). On first run it imports the Node relay's relay/data so
// already-paired phones keep working.
//
// Owner: WorkBuddy.
package store
