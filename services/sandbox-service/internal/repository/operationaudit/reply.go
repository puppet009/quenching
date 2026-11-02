package operationaudit

import (
	"context"
)

func (s *PGStore) LinkReply(ctx context.Context, sid int64, ids []string, replyID int64) error {
	for _, id := range ids {
		_, err := s.exec(ctx, `INSERT INTO operation_reply_links(operation_id,reply_message_id) SELECT operation_id,$3 FROM operation_intents WHERE operation_id=$1 AND session_id=$2 ON CONFLICT DO NOTHING`, id, sid, replyID)
		if err != nil {
			return err
		}
	}
	return nil
}
