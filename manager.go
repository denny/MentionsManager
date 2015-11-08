package main

import (
	"fmt"
	"log"
	"regexp"
	"net/url"
	
	"./smtp-with-self-signed-cert"			// Standard lib can't support this *eye-roll*
	
	"gopkg.in/gcfg.v1"						// Config file parser
	"github.com/ChimeraCoder/anaconda"		// Twitter API
)


// Config
type Config struct {
	Auth struct {
		ConsumerKey       string
		ConsumerSecret    string
		AccessToken       string
		AccessTokenSecret string
	}
	Auth2 struct {
		ConsumerKey       string
		ConsumerSecret    string
		AccessToken       string
		AccessTokenSecret string
	}
	Settings struct {
		FriendsCount      string
		FollowerCount     string
		MentionsCount     string
		MyScreenName      string
		ReasonsURL        string
	}
	Email struct {
		SendEmails        bool
		Server            string
		ServerPort        string
		AuthUsername      string
		AuthPassword      string
		FromAddress       string
		AdminAddress      string
	}
}


// Load config, connect to Twitter API, fetch and check mentions, 
// block and notify users where necessary
func main() {
	// Get config from file
	cfg := getConfig( "config.gcfg" )
	
	// Initialise and authenticate API objects
	anaconda.SetConsumerKey( cfg.Auth.ConsumerKey )
	anaconda.SetConsumerSecret( cfg.Auth.ConsumerSecret )
	api := anaconda.NewTwitterApi( cfg.Auth.AccessToken, cfg.Auth.AccessTokenSecret )
	
	// Get friends' and followers' IDs
	friendsIds   := getFriendIDs(    cfg.Settings.FriendsCount,  api )
	followersIds := getFollowersIDs( cfg.Settings.FollowerCount, api )
	
	// Get mentions
	mentions := getMentions( cfg.Settings.MentionsCount, api )
	
	// Run through mentions checking against friend/follower lists and block criteria
	for _ , tweet := range mentions {
		fmt.Println( "<" + tweet.User.ScreenName + ">", tweet.Text )
		// Check to see if tweet was posted by a friend
		if ! friendsIds[ tweet.User.Id ] && ! followersIds[ tweet.User.Id ] {
			// Not a friend or follower - check the tweet content against block rules
			checkTweet( tweet, cfg, api )
		}
	}
}


// Read config file
func getConfig( filename string ) ( *Config ) {
	cfg := new( Config )
	cfgerr := gcfg.ReadFileInto( cfg, filename )
	if cfgerr != nil { log.Fatalf( "Failed to parse gcfg data: %s", cfgerr ) }
	return( cfg )
}


// Get friends' user IDs
func getFriendIDs( friendsCount string, api *anaconda.TwitterApi ) ( map[int64]bool ) {
	params := url.Values{}
	params.Set( "count", friendsCount )
	
	// Get the list of IDs from the API
	friendsIdsAll, err := api.GetFriendsIds( params )
	if err != nil { log.Fatalf( "Failed to get friends data: %s", err ) }
	friendsIdsTemp := friendsIdsAll.Ids
	
	// Create a map
	friendsIds := make( map[int64]bool )
	for _ , friendId := range friendsIdsTemp {
		friendsIds[ friendId ] = true
	}
	
	return( friendsIds )
}


// Get followers' user IDs
func getFollowersIDs( followerCount string, api *anaconda.TwitterApi ) ( map[int64]bool ) {
	params := url.Values{}
	params.Set( "count", followerCount )
	
	// Get the list of IDs from the API
	followerIdsAll, err := api.GetFollowersIds( params )
	if err != nil { log.Fatalf( "Failed to get followers data: %s", err ) }
	followerIdsTemp := followerIdsAll.Ids
	
	// Create a map
	followerIds := make( map[int64]bool )
	for _ , followerId := range followerIdsTemp {
		followerIds[ followerId ] = true
	}
	
	return( followerIds )
}


// Get mentions
func getMentions( mentionsCount string, api *anaconda.TwitterApi ) ( []anaconda.Tweet ) { 
	params := url.Values{}
	params.Set( "count", mentionsCount )
	
	// TODO: Track tweet ID we checked up to last time, and check from there onwards
	
	mentions, err := api.GetMentionsTimeline( params )
	if err != nil { log.Fatalf( "Failed to get mentions: %s", err ) }
	
	return( mentions )
}


// Check a tweet to see if it matches any of the block rules
// TODO: Pull the rules out into a config file (or database?)
func checkTweet( tweet anaconda.Tweet, cfg *Config, api *anaconda.TwitterApi ) {
	// Body text
	textRules := map[string]string { 
		// Celebrities
		"(?i)Hamlin":  "dennyhamlin",	// Denny Hamlin - NASCAR driver
		"(?i)NASCAR":  "dennyhamlin",	// ""
		"(?i)Cagur":   "dennycagur",	// Denny Cagur - Indonesian actor
		"(?i)Sumargo": "dennysumargo",	// Denny Sumargo - Indonesian basketball player
		"(?i)Gitong":  "dennygitong",	// Denny Gitong - Indonesian comedian
		// Denny's Diner
		"(?i)^@Denny's$": "atdennys",
		"(?i)@Denny's.+(breakfast|lunch|dinner|food|coffee|milkshake|Grand Slam|diner|waitress|service|smash|IHOP)": "dennysdiner",
		"(?i)(breakfast|lunch|dinner|food|coffee|milkshake|Grand Slam|diner|waitress|service|fam |smash|IHOP|LIVE on #Periscope).+@Denny's": "dennysdiner" }
	
	for rule, ruleName := range textRules {
		match, err := regexp.MatchString( rule, tweet.Text )
		if err != nil { log.Fatalf( "Regexp failed: %s", err ) }
		if match {
			blockUser( tweet, ruleName, cfg, api )
			if cfg.Email.SendEmails {
				emailNotification( tweet, ruleName, cfg )
			}
			return
		}
	}
	
	// Location
	locationRules := map[string]string {
		// Indonesia in general is a problem for me on Twitter, unfortunately!
		"(?i)Indonesia": "indonesia",
		"(?i)Jakarta":   "indonesia",
		"(?i)Bandung":   "indonesia",
		"(?i)Padang":    "indonesia",
	}
	
	for rule, ruleName := range locationRules {
		location := tweet.Place.Country + " " + tweet.Place.Name + " " + tweet.Place.FullName
		match, err := regexp.MatchString( rule, location )
		if err != nil { log.Fatalf( "Regexp failed: %s", err ) }
		if match {
			blockUser( tweet, ruleName, cfg, api )
			if cfg.Email.SendEmails {
				emailNotification( tweet, ruleName, cfg )
			}
			return
		}
	}
}


// Block a user, and tweet a notification of why they were blocked
func blockUser( tweet anaconda.Tweet, ruleName string, cfg *Config, api *anaconda.TwitterApi ) {
	// Block the user from the main account
	user, err1 := api.BlockUserId( tweet.User.Id, nil )
	if err1 != nil { log.Fatalf( "Failed to block user: %s", err1 ) }
	
	// Let them know via the notification account
	anaconda.SetConsumerKey( cfg.Auth2.ConsumerKey )
	anaconda.SetConsumerSecret( cfg.Auth2.ConsumerSecret )
	api2 := anaconda.NewTwitterApi( cfg.Auth2.AccessToken, cfg.Auth2.AccessTokenSecret )
	
	// TODO: Make this work...
	params := url.Values{}
	params.Set( "InReplyToStatusID",    tweet.IdStr )
	params.Set( "InReplyToStatusIdStr", tweet.IdStr )
	
	tweet2, err2 := api2.PostTweet( "@" + user.ScreenName + 
		": Hi! You've been blocked by @" + cfg.Settings.MyScreenName  + 
		". Reason: " + cfg.Settings.ReasonsURL + "#" + ruleName, params )
	if err2 != nil { log.Fatalf( "Failed to notify blocked user: %s", err2 ) }
	
	// Display tweet in terminal
	fmt.Println( ">> " + tweet2.Text )
	
	// Restore API to main account auth settings
	anaconda.SetConsumerKey( cfg.Auth.ConsumerKey )
	anaconda.SetConsumerSecret( cfg.Auth.ConsumerSecret )
}


// Send an email letting admin know that bot did something
func emailNotification( tweet anaconda.Tweet, ruleName string, cfg *Config ) {
	// Create the email body text
	body := "From: "    + cfg.Email.FromAddress  + "\n" +
			"To: "      + cfg.Email.AdminAddress + "\n" +
			"Subject: " + "[BlockBot] Blocked @" + tweet.User.ScreenName +
			"\n" +
			"Your block bot just blocked @" + tweet.User.ScreenName +
			" for the following tweet: \n" +
			tweet.Text + "\n" +
			"\n" +
			"User:  https://twitter.com/" + tweet.User.ScreenName + "\n" +
			"Tweet: https://twitter.com/" + tweet.User.ScreenName +
			"/status/" + tweet.IdStr + "\n"
	// Display email in terminal too
	fmt.Println( body )
	
	// Set up authentication details
	auth := smtp.PlainAuth(
		"",
		cfg.Email.AuthUsername,
		cfg.Email.AuthPassword,
		cfg.Email.Server,
	)
	
	// Connect to SMTP server and send the email
	err := smtp.SendMail(
		cfg.Email.Server + ":" + cfg.Email.ServerPort,
		auth,
		cfg.Email.FromAddress,
		[]string{ cfg.Email.AdminAddress },
		[]byte( body ),
	)
	if err != nil { log.Fatal( err ) }
}

