package mongo

import "go.mongodb.org/mongo-driver/mongo"

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	writeException, ok := err.(mongo.WriteException)
	if !ok {
		return false
	}
	for _, writeErr := range writeException.WriteErrors {
		if writeErr.Code == 11000 {
			return true
		}
	}
	return false
}
