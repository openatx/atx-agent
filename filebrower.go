package main

import (
	"net/http"
	"path/filepath"

	"github.com/asdine/storm"
	"github.com/filebrowser/filebrowser/v2/auth"
	"github.com/filebrowser/filebrowser/v2/errors"
	fhttp "github.com/filebrowser/filebrowser/v2/http"
	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/storage"
	"github.com/filebrowser/filebrowser/v2/storage/bolt"
	"github.com/filebrowser/filebrowser/v2/users"
)

var databases = map[string]*storm.DB{}

func fbHandler(root string) http.Handler {
	values := map[string]string{
		"baseURL":          "/fb/",
		"root":             root,
		"database":         filepath.Join(filepath.Dir(root), "filebrower.db"),
		"auth_method":      "json",
		"auth_header":      "",
		"recaptcha_host":   "",
		"recaptcha_key":    "",
		"recaptcha_secret": "",
	}

	var err error

	ser := &settings.Server{
		Root:    values["root"],
		BaseURL: values["baseURL"],
	}

	ser.Root, err = filepath.Abs(ser.Root)
	if err != nil {
		log.Printf("get root(%s) error (%s)\n", ser.Root, err)
		return nil
	}

	var (
		db *storm.DB
		ok bool
	)

	if db, ok = databases[values["database"]]; !ok {
		db, err = storm.Open(values["database"])
		if err != nil {
			return nil
		}
		databases[values["database"]] = db
	}

	sto, err := bolt.NewStorage(db)
	if err != nil {
		log.Printf("create db error (%s)\n", err)
		return nil
	}

	set, err := sto.Settings.Get()
	if err == errors.ErrNotExist {
		err = quickSetup(sto, values)
		if err != nil {
			log.Printf("quick setup error (%s)\n", err)
			return nil
		}

		set, err = sto.Settings.Get()
	}

	if err != nil {
		return nil
	}

	var auther auth.Auther

	switch settings.AuthMethod(values["auth_method"]) {
	case auth.MethodJSONAuth:
		set.AuthMethod = auth.MethodJSONAuth
		auther = &auth.JSONAuth{
			ReCaptcha: &auth.ReCaptcha{
				Host:   values["recaptcha_host"],
				Key:    values["recaptcha_key"],
				Secret: values["recaptcha_secret"],
			},
		}
	case auth.MethodNoAuth:
		set.AuthMethod = auth.MethodNoAuth
		auther = &auth.NoAuth{}
	case auth.MethodProxyAuth:
		set.AuthMethod = auth.MethodProxyAuth
		header := values["auth_header"]
		if header == "" {
			return nil
		}
		auther = &auth.ProxyAuth{Header: header}
	default:
		return nil
	}

	err = sto.Settings.Save(set)
	if err != nil {
		log.Printf("save setting error (%s)\n", err)
		return nil
	}

	err = sto.Settings.SaveServer(ser)
	if err != nil {
		log.Printf("save server error (%s)\n", err)
		return nil
	}

	err = sto.Auth.Save(auther)
	if err != nil {
		log.Printf("save auth error (%s)\n", err)
		return nil
	}

	httpHandler, err := fhttp.NewHandler(sto, ser)
	if err != nil {
		log.Printf("create handler error (%s)\n", err)
		return nil
	}

	return httpHandler
}

func quickSetup(sto *storage.Storage, values map[string]string) error {
	key, err := settings.GenerateKey()
	if err != nil {
		return err
	}

	set := &settings.Settings{
		Key:    key,
		Signup: false,
		Defaults: settings.UserDefaults{
			Scope:  ".",
			Locale: "en",
			Perm: users.Permissions{
				Admin:    false,
				Execute:  true,
				Create:   true,
				Rename:   true,
				Modify:   true,
				Delete:   true,
				Share:    true,
				Download: true,
			},
		},
	}

	err = sto.Settings.Save(set)
	if err != nil {
		return err
	}

	password, err := users.HashPwd("sofire")
	if err != nil {
		return err
	}

	user := &users.User{
		Username:     "sofire",
		Password:     password,
		LockPassword: false,
	}

	set.Defaults.Apply(user)
	user.Perm.Admin = true

	return sto.Users.Save(user)
}
