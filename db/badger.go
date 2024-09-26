package db

import (
	"time"
	"tiny-counts/models"

	"github.com/timshannon/badgerhold"
)

type BadgerDB struct {
	Store *badgerhold.Store
}

func NewBadgerDB(path string) (*BadgerDB, error) {
	options := badgerhold.DefaultOptions
	options.Dir = path
	options.ValueDir = path

	store, err := badgerhold.Open(options)
	if err != nil {
		return nil, err
	}
	return &BadgerDB{Store: store}, nil
}

func (b *BadgerDB) Close() error {
	return b.Store.Close()
}

func (b *BadgerDB) IncrementCounter(key string, count int64) error {
	record := &models.Record{}
	err := b.Store.Get(key, record)
	if err != nil {
		return err
	}
	if record.Key == key {
		record.Count += count
		return b.Store.Update(key, record)
	}
	record.Key = key
	record.Timestamp = time.Now()
	record.Count = count
	return b.Store.Insert(key, record)
}

func (b *BadgerDB) AggregateCounts(key string, startTime, endTime time.Time) (int64, error) {
	query := badgerhold.Where("Key").Eq(key).And("Timestamp").Ge(startTime).And("Timestamp").Le(endTime)
	var records []models.Record
	if err := b.Store.Find(&records, query); err != nil {
		if err == badgerhold.ErrNotFound {
			return 0, nil
		}
		return 0, err
	}

	total := int64(0)
	for _, r := range records {
		total += r.Count
	}
	return total, nil
}

func (b *BadgerDB) GetAllKeys() ([]string, error) {
	var keys []string
	var records []models.Record
	err := b.Store.Find(&records, nil)
	for _, record := range records {
		keys = append(keys, record.Key)
	}
	return keys, err
}
