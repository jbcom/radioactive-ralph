package ipc

import "context"

// ObserveSnapshot reads one project-scoped, versioned, content-safe operator
// snapshot from the supervisor. An old supervisor returns
// CodeUnsupportedCommand; clients must report an upgrade requirement and must
// not reopen SQLite.
func (c *Client) ObserveSnapshot(
	ctx context.Context,
	args ObserveSnapshotArgs,
) (*ObserveSnapshotReply, error) {
	var reply ObserveSnapshotReply
	if err := c.versionedCall(
		ctx,
		QueryProtoVersion,
		CmdObserveSnapshot,
		args,
		&reply,
	); err != nil {
		return nil, err
	}
	return &reply, nil
}

// ObserveMessages reads one bounded chronological page of content-free A2A
// message metadata.
func (c *Client) ObserveMessages(
	ctx context.Context,
	args ObserveMessagesArgs,
) (*ObserveMessagesReply, error) {
	var reply ObserveMessagesReply
	if err := c.versionedCall(
		ctx,
		QueryProtoVersion,
		CmdObserveMessages,
		args,
		&reply,
	); err != nil {
		return nil, err
	}
	return &reply, nil
}
