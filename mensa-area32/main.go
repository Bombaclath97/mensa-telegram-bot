package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/tebeka/selenium"
)

const (
	seleniumPath    = "path/to/selenium-server-standalone.jar"
	geckoDriverPath = "path/to/geckodriver"
	port            = 8080
	dbPath          = "./entries.db"
)

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Initialize the database schema
	initDB()

	// Check if the database is older than 24 hours
	if isDBExpired() {
		err := scrapeWebsite()
		if err != nil {
			log.Fatalf("failed to scrape website: %v", err)
		}
	}

	http.HandleFunc("/retrieve", retrieveHandler)
	log.Printf("Starting server on :%d...\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func initDB() {
	stmt := `
    CREATE TABLE IF NOT EXISTS entries (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        last_name TEXT NOT NULL,
        info TEXT NOT NULL,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`
	_, err := db.Exec(stmt)
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}
}

func isDBExpired() bool {
	var updatedAt time.Time
	err := db.QueryRow("SELECT updated_at FROM entries ORDER BY updated_at DESC LIMIT 1").Scan(&updatedAt)
	if err != nil && err != sql.ErrNoRows {
		log.Fatalf("failed to check database expiration: %v", err)
	}
	return time.Since(updatedAt) > 24*time.Hour
}

func scrapeWebsite() error {
	// Start a Selenium WebDriver server instance
	opts := []selenium.ServiceOption{
		selenium.GeckoDriver(geckoDriverPath), // Specify the path to GeckoDriver
	}
	service, err := selenium.NewSeleniumService(seleniumPath, port, opts...)
	if err != nil {
		return fmt.Errorf("error starting Selenium service: %v", err)
	}
	defer service.Stop()

	// Connect to the WebDriver instance running locally
	caps := selenium.Capabilities{"browserName": "firefox"}
	driver, err := selenium.NewRemote(caps, fmt.Sprintf("http://localhost:%d/wd/hub", port))
	if err != nil {
		return fmt.Errorf("error connecting to WebDriver: %v", err)
	}
	defer driver.Quit()

	// Log in to the website
	if err := login(driver); err != nil {
		return fmt.Errorf("error logging in: %v", err)
	}

	// Navigate to the specific page and scrape the entries
	if err := scrapeEntries(driver); err != nil {
		return fmt.Errorf("error scraping entries: %v", err)
	}

	return nil
}

func login(driver selenium.WebDriver) error {
	// Replace with the actual URL and login credentials
	url := "https://example.com/login"
	username := "your-username"
	password := "your-password"

	if err := driver.Get(url); err != nil {
		return err
	}

	// Find and fill the username field
	usernameField, err := driver.FindElement(selenium.ByID, "username")
	if err != nil {
		return err
	}
	if err := usernameField.SendKeys(username); err != nil {
		return err
	}

	// Find and fill the password field
	passwordField, err := driver.FindElement(selenium.ByID, "password")
	if err != nil {
		return err
	}
	if err := passwordField.SendKeys(password); err != nil {
		return err
	}

	// Find and click the login button
	loginButton, err := driver.FindElement(selenium.ByID, "loginButton")
	if err != nil {
		return err
	}
	if err := loginButton.Click(); err != nil {
		return err
	}

	return nil
}

func scrapeEntries(driver selenium.WebDriver) error {
	// Replace with the actual URL of the page to navigate to
	url := "https://example.com/specific-page"
	if err := driver.Get(url); err != nil {
		return err
	}

	// Find the table rows
	rows, err := driver.FindElements(selenium.ByCSSSelector, "table#entries tbody tr")
	if err != nil {
		return err
	}

	// Clear existing entries
	_, err = db.Exec("DELETE FROM entries")
	if err != nil {
		return err
	}

	// Iterate over the rows and extract the data
	for _, row := range rows {
		columns, err := row.FindElements(selenium.ByTagName, "td")
		if err != nil {
			return err
		}

		name, err := columns[0].Text()
		if err != nil {
			return err
		}

		lastName, err := columns[1].Text()
		if err != nil {
			return err
		}

		info, err := columns[2].Text()
		if err != nil {
			return err
		}

		_, err = db.Exec("INSERT INTO entries (name, last_name, info) VALUES (?, ?, ?)", name, lastName, info)
		if err != nil {
			return err
		}
	}

	return nil
}

func retrieveHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	lastName := r.URL.Query().Get("lastName")

	if name == "" || lastName == "" {
		http.Error(w, "Missing name or lastName parameter", http.StatusBadRequest)
		return
	}

	var info string
	err := db.QueryRow("SELECT info FROM entries WHERE name = ? AND last_name = ?", name, lastName).Scan(&info)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Entry not found", http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Error retrieving entry: %v", err), http.StatusInternalServerError)
		}
		return
	}

	fmt.Fprintf(w, "Retrieved entry: %s", info)
}
