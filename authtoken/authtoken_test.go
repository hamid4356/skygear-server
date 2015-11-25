package authtoken

import (
	"fmt"
	"io/ioutil"
	"log"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"bytes"
	"os"
	"path/filepath"
	"time"
)

func tempDir() string {
	dir, err := ioutil.TempDir("", "skydb.auth.test")
	if err != nil {
		panic(err)
	}
	return dir
}

func TestNewToken(t *testing.T) {
	token := New("com.oursky.skygear", "46709394", time.Time{})

	if token.AppName != "com.oursky.skygear" {
		t.Fatalf("got token.AppName = %v, want com.oursky.skygear", token.AppName)
	}

	if token.UserInfoID != "46709394" {
		t.Fatalf("got token.UserInfoID = %v, want 46709394", token.UserInfoID)
	}

	if token.AccessToken == "" {
		t.Fatal("got empty token, want non-empty AccessToken value")
	}

	if token.ExpiredAt.IsZero() {
		t.Fatalf("got token = %v, want non-zero ExpiredAt value", token)
	}
}

func TestNewTokenWithExpiry(t *testing.T) {
	expiredAt := time.Unix(0, 1)

	token := New("com.oursky.skygear", "46709394", expiredAt)

	if !token.ExpiredAt.Equal(expiredAt) {
		t.Fatalf("got token.ExpiredAt = %v, want %v", token.ExpiredAt, expiredAt)
	}
}

func TestTokenIsExpired(t *testing.T) {
	now := time.Now()
	token := Token{}

	token.ExpiredAt = now.Add(1 * time.Second)
	if token.IsExpired() {
		t.Fatalf("got expired token = %v, now = %v, want it not expired", token, now)
	}

	token.ExpiredAt = now.Add(-1 * time.Second)
	if !token.IsExpired() {
		t.Fatalf("got non-expired token = %v, now = %v, want it expired", token, now)
	}
}

func TestEmptyTokenIsExpired(t *testing.T) {
	token := Token{}
	if !token.IsExpired() {
		t.Fatalf("got non-expired empty token = %v, want it expired", token)
	}
}

func TestFileStorePut(t *testing.T) {
	const savedFileContent = `{"accessToken":"sometoken","expiredAt":1000000001,"appName":"com.oursky.skygear","userInfoID":"someuserinfoid"}
`
	token := Token{
		AccessToken: "sometoken",
		ExpiredAt:   time.Unix(1, 1).UTC(),
		AppName:     "com.oursky.skygear",
		UserInfoID:  "someuserinfoid",
	}

	dir := tempDir()
	defer os.RemoveAll(dir)

	store := FileStore(dir)
	if err := store.Put(&token); err != nil {
		t.Fatalf("got err = %v, want nil", err)
	}

	filePath := filepath.Join(dir, "sometoken")
	file, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}

	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		panic(err)
	}

	if !bytes.Equal(fileBytes, []byte(savedFileContent)) {
		t.Fatalf("got file content = %#v, want %#v", string(fileBytes), savedFileContent)
	}
}

func TestFileStoreGet(t *testing.T) {
	Convey("FileStore", t, func() {
		dir := tempDir()
		defer os.RemoveAll(dir)

		store := FileStore(dir)
		token := Token{}

		Convey("gets an non-expired file token", func() {
			tomorrow := time.Now().AddDate(0, 0, 1)

			So(store.Put(&Token{
				AccessToken: "sometoken",
				ExpiredAt:   tomorrow,
				AppName:     "com.oursky.skygear",
				UserInfoID:  "someuserinfoid",
			}), ShouldBeNil)

			err := store.Get("sometoken", &token)
			So(err, ShouldBeNil)

			So(token, ShouldResemble, Token{
				AccessToken: "sometoken",
				ExpiredAt:   tomorrow,
				AppName:     "com.oursky.skygear",
				UserInfoID:  "someuserinfoid",
			})
		})

		Convey("returns an NotFoundError when the token to get is expired", func() {
			yesterday := time.Now().AddDate(0, 0, -1)
			tokenString := fmt.Sprintf(`
{
	"accessToken": "sometoken",
	"expiredAt": %v,
	"appName": "com.oursky.skygear",
	"userInfoID": "someuserinfoid"
}
			`, yesterday.UnixNano())

			err := ioutil.WriteFile(filepath.Join(dir, "sometoken"), []byte(tokenString), 0644)
			So(err, ShouldBeNil)

			err = store.Get("sometoken", &token)
			So(err, ShouldHaveSameTypeAs, &NotFoundError{})

			Convey("and deletes the token file", func() {
				_, err := os.Stat(filepath.Join(dir, "sometoken"))
				So(os.IsNotExist(err), ShouldBeTrue)
			})
		})

		Convey("returns a NotFoundError when the token to get does not existed", func() {
			err := store.Get("notexisttoken", &token)
			So(err, ShouldHaveSameTypeAs, &NotFoundError{})
		})
	})
}

func TestFileStoreEscape(t *testing.T) {
	Convey("FileStore", t, func() {
		tDir := tempDir()
		defer os.RemoveAll(tDir)

		dir := filepath.Join(tDir, "inner")
		mdErr := os.Mkdir(dir, 0755)
		So(mdErr, ShouldBeNil)

		store := FileStore(dir)
		token := Token{}

		Convey("Get not escaping dir", func() {
			outterFilepath := filepath.Join(tDir, "outerfile")
			err := ioutil.WriteFile(outterFilepath, []byte(`{}`), 0644)
			So(err, ShouldBeNil)

			err = store.Get("../outerfile", &token)
			So(err.Error(), ShouldEqual, `get "../outerfile": invalid access token`)
		})

		Convey("Put not escaping dir", func() {
			token := Token{
				AccessToken: "../outerfile",
				ExpiredAt:   time.Unix(1, 1).UTC(),
				AppName:     "com.oursky.skygear",
				UserInfoID:  "someuserinfoid",
			}
			err := store.Put(&token)
			So(err.Error(), ShouldEqual, `get "../outerfile": invalid access token`)
		})

		Convey("Delete not escaping dir", func() {
			outterFilepath := filepath.Join(tDir, "outerfile")
			err := ioutil.WriteFile(outterFilepath, []byte(`{}`), 0644)
			So(err, ShouldBeNil)

			err = store.Delete("../outerfile")
			So(err.Error(), ShouldEqual, `get "../outerfile": invalid access token`)
		})
	})
}

func TestFileStoreDelete(t *testing.T) {
	Convey("FileStore", t, func() {
		dir := tempDir()
		// defer os.RemoveAll(dir)
		store := FileStore(dir)

		Convey("delete an existing token", func() {
			accessTokenPath := filepath.Join(dir, "accesstoken")
			log.Println(accessTokenPath)

			So(ioutil.WriteFile(accessTokenPath, []byte(`{}`), 0644), ShouldBeNil)
			So(exists(accessTokenPath), ShouldBeTrue)

			err := store.Delete("accesstoken")
			So(err, ShouldBeNil)

			So(exists(accessTokenPath), ShouldBeFalse)
		})

		Convey("delete an not existing token", func() {
			err := store.Delete("notexistaccesstoken")
			So(err, ShouldBeNil)
		})

		Convey("delete an empty token", func() {
			err := store.Delete("")
			So(err, ShouldHaveSameTypeAs, &NotFoundError{})
			So(err.Error(), ShouldEqual, `get "": invalid access token`)
		})
	})
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)

}

func tempRedisStore() *RedisStore {
	// 15 is the default max DB number of redis
	return NewRedisStore("redis://127.0.0.1:6379/15")
}

func (r *RedisStore) clearRedisStore() {
	c := r.pool.Get()
	defer c.Close()

	c.Do("FLUSHDB")
}

func TestRedisStoreGet(t *testing.T) {
	Convey("RedisStore", t, func() {
		r := tempRedisStore()
		defer r.clearRedisStore()

		Convey("Get Non-Expired Token", func() {
			tokenName := "someToken"
			tomorrow := time.Now().AddDate(0, 0, 1).UTC()
			token := Token{
				AccessToken: tokenName,
				ExpiredAt:   tomorrow,
				AppName:     "com.oursky.skygear",
				UserInfoID:  "someuserinfoid",
			}
			err := r.Put(&token)
			So(err, ShouldBeNil)

			result := Token{}
			err = r.Get(tokenName, &result)
			So(err, ShouldBeNil)
			So(result, ShouldResemble, token)
		})

		Convey("Get Expired Token", func() {
			tokenName := "expiredToken"
			yesterday := time.Now().AddDate(0, 0, -1).UTC()
			token := Token{
				AccessToken: tokenName,
				ExpiredAt:   yesterday,
				AppName:     "com.oursky.skygear",
				UserInfoID:  "someuserinfoid",
			}
			err := r.Put(&token)
			So(err, ShouldBeNil)

			result := Token{}
			err = r.Get(tokenName, &result)
			So(err, ShouldHaveSameTypeAs, &NotFoundError{})
		})

		Convey("Get Updated Token", func() {
			tokenName := "updatedToken"
			tomorrow := time.Now().AddDate(0, 0, 1).UTC()
			token := Token{
				AccessToken: tokenName,
				ExpiredAt:   tomorrow,
				AppName:     "com.oursky.skygear",
				UserInfoID:  "someuserinfoid",
			}
			err := r.Put(&token)
			So(err, ShouldBeNil)

			result := Token{}
			err = r.Get(tokenName, &result)
			So(err, ShouldBeNil)
			So(result, ShouldResemble, token)

			Convey("update to future", func() {
				future := time.Now().AddDate(0, 0, 10).UTC()
				token := Token{
					AccessToken: tokenName,
					ExpiredAt:   future,
					AppName:     "com.oursky.skygear",
					UserInfoID:  "someuserinfoid",
				}
				err := r.Put(&token)
				So(err, ShouldBeNil)

				result := Token{}
				err = r.Get(tokenName, &result)
				So(err, ShouldBeNil)
				So(result, ShouldResemble, token)
			})

			Convey("update to the past", func() {
				past := time.Now().AddDate(0, 0, -10).UTC()
				token := Token{
					AccessToken: tokenName,
					ExpiredAt:   past,
					AppName:     "com.oursky.skygear",
					UserInfoID:  "someuserinfoid",
				}
				err := r.Put(&token)
				So(err, ShouldBeNil)

				result := Token{}
				err = r.Get(tokenName, &result)
				So(err, ShouldHaveSameTypeAs, &NotFoundError{})
			})
		})

		Convey("Get Nonexistent Token", func() {
			tokenName := "nonexistentToken"

			result := Token{}
			err := r.Get(tokenName, &result)
			So(err, ShouldHaveSameTypeAs, &NotFoundError{})
		})
	})
}

func TestRedisStorePut(t *testing.T) {
	Convey("RedisStore", t, func() {
		tokenName := ""
		r := tempRedisStore()
		defer r.clearRedisStore()

		tomorrow := time.Now().AddDate(0, 0, 1).UTC()
		token := Token{
			AccessToken: tokenName,
			ExpiredAt:   tomorrow,
			AppName:     "com.oursky.skygear",
			UserInfoID:  "someuserinfoid",
		}

		err := r.Put(&token)
		So(err, ShouldBeNil)
	})
}

func TestRedisStoreDelete(t *testing.T) {
	Convey("RedisStore", t, func() {
		r := tempRedisStore()
		defer r.clearRedisStore()

		Convey("Delete existing token", func() {
			tokenName := "someToken"
			tomorrow := time.Now().AddDate(0, 0, 1).UTC()
			token := Token{
				AccessToken: tokenName,
				ExpiredAt:   tomorrow,
				AppName:     "com.oursky.skygear",
				UserInfoID:  "someuserinfoid",
			}
			err := r.Put(&token)
			So(err, ShouldBeNil)

			err = r.Delete(tokenName)
			So(err, ShouldBeNil)

			result := Token{}
			err = r.Get(tokenName, &result)
			So(err, ShouldHaveSameTypeAs, &NotFoundError{})
		})

		Convey("Delete nonexistent token", func() {
			tokenName := "nonexistentToken"
			err := r.Delete(tokenName)
			So(err, ShouldBeNil)
		})
	})
}
