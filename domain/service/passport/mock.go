package passport

import (
	"context"
	"sync"

	jsoniter "github.com/json-iterator/go"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/sirupsen/logrus"
)

var (
	defaultMockData *MockData
	mockLock        sync.Mutex
)

func GetMockData() *MockData {
	mockLock.Lock()
	defer mockLock.Unlock()
	if defaultMockData == nil {
		defaultMockData = &MockData{}
	}
	return defaultMockData
}

type MockData struct {
	Lock           sync.RWMutex
	UserIdentities []*model.UserIdentity

	// cache
	mobileMap   map[string]*model.UserIdentity
	emailMap    map[string]*model.UserIdentity
	userNameMap map[string]*model.UserIdentity
}

func (m *MockData) LoadConf(conf string) error {

	if len(conf) == 0 {
		logrus.Warnf("mock data init with param length 0")
	}

	var identities []*model.UserIdentity
	err := jsoniter.Unmarshal([]byte(conf), &identities)
	if err != nil {
		return err
	}

	m.Lock.Lock()
	defer m.Lock.Unlock()

	m.UserIdentities = identities
	// clean cache
	m.mobileMap = make(map[string]*model.UserIdentity, len(identities))
	m.emailMap = make(map[string]*model.UserIdentity, len(identities))
	m.userNameMap = make(map[string]*model.UserIdentity, len(identities))

	for _, i := range identities {
		if len(i.Mobile) > 0 {
			m.mobileMap[i.Mobile] = i
		}
		if len(i.Email) > 0 {
			m.emailMap[i.Email] = i
		}
		if len(i.UserName) > 0 {
			m.userNameMap[i.UserName] = i
		}
	}
	return nil
}

func (m *MockData) GetIdentity(ctx context.Context, mobileEmailUsername string) (res *model.UserIdentity, err error) {
	var ok bool
	res, ok = m.mobileMap[mobileEmailUsername]
	if ok {
		return
	}
	res, ok = m.emailMap[mobileEmailUsername]
	if ok {
		return
	}
	res, ok = m.userNameMap[mobileEmailUsername]
	if ok {
		return
	}
	return nil, nil
}
