package models

import (
    "time"
)

type Record struct {
    Key       string    `badgerhold:"Key"`
    Timestamp time.Time `badgerhold:"Timestamp"`
    Count     int64     `badgerhold:"Count"`
}
