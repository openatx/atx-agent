package main

// type uiautomatorLauncher struct {
// 	running bool
// }

// func (u *uiautomatorLauncher) Start() error {
// 	if u.running {
// 		return errors.New("uiautomator already started")
// 	}
// 	if runtime.GOOS == "windows" {
// 		u.running = true
// 		return nil
// 	}
// 	go u.safeRun()
// 	return nil
// }

// func (u *uiautomatorLauncher) IsRunning() bool {
// 	return u.running
// }

// func (u *uiautomatorLauncher) safeRun() {
// 	u.running = true
// 	retry := 5
// 	for retry > 0 {
// 		retry--
// 		start := time.Now()
// 		if err := u.runUiautomator(); err != nil {
// 			log.Printf("uiautomator quit: %v", err)
// 		}
// 		if time.Since(start) > 1*time.Minute {
// 			retry = 5
// 		}
// 		time.Sleep(2 * time.Second)
// 	}
// 	log.Println("uiautomator can not started")
// 	u.running = false
// }

// func (u *uiautomatorLauncher) runUiautomator() error {
// 	c := exec.Command("am", "instrument", "-w", "-r",
// 		"-e", "debug", "false",
// 		"-e", "class", "com.github.uiautomator.stub.Stub",
// 		"com.github.uiautomator.test/android.support.test.runner.AndroidJUnitRunner")
// 	c.Stdout = os.Stdout
// 	c.Stderr = os.Stderr
// 	return c.Run()
// }

// var uiautomator uiautomatorLauncher

// func safeRunUiautomator() error {
// 	return uiautomator.Start()
// }
