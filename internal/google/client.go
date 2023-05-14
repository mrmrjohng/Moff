package google

import (
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
	"moff.io/moff-social/internal/config"
	"moff.io/moff-social/pkg/errors"
	"moff.io/moff-social/pkg/log"
	"sync"
)

type Clients struct {
	sheet *sheets.Service
	drive *drive.Service
}

var (
	initClientOnce sync.Once
	internalClient *Clients
)

//NewClients returns google sheets\drive service wrapper.
func NewClients() *Clients {
	initClientOnce.Do(func() {
		ctx := context.Background()
		conf := oauth2Config(&config.Global.Google.Credential, sheets.SpreadsheetsScope, drive.DriveScope)
		client := conf.Client(context.Background(), oauth2Token(&config.Global.Google.Oauth2Token))
		sheet, err := sheets.NewService(ctx, option.WithHTTPClient(client),
			option.WithScopes(sheets.SpreadsheetsScope))

		if err != nil {
			log.Fatal(errors.WrapAndReport(err, "retrieve google sheets client"))
		}
		driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(client),
			option.WithScopes(drive.DriveScope))
		if err != nil {
			log.Fatal(errors.WrapAndReport(err, "retrieve google drive client"))
		}
		internalClient = &Clients{
			sheet: sheet,
			drive: driveSrv,
		}
		log.Info("Google client initialized...")
	})
	return internalClient
}

func (cli *Clients) CreateSpreadsheet(title string) (*sheets.Spreadsheet, error) {
	spreadsheet, err := cli.sheet.Spreadsheets.Create(&sheets.Spreadsheet{
		Properties: &sheets.SpreadsheetProperties{
			Title:  title,
			Locale: "en",
		},
	}).Do()
	if err != nil {
		return nil, errors.WrapAndReport(err, "create google sheet")
	}
	return spreadsheet, nil
}

func (cli *Clients) ShareFileToAnyReader(fileID string) error {
	_, err := cli.drive.Permissions.Create(fileID, &drive.Permission{
		Kind: "drive#permission",
		Role: "reader",
		Type: "anyone",
	}).Do()
	return errors.WrapAndReport(err, "share file reader to anyone")
}

type SpreadsheetPushRequest struct {
	SpreadsheetId string          `json:"spreadsheet_id"`
	Range         string          `json:"range"`
	Values        [][]interface{} `json:"values"`
}

func (cli *Clients) AppendRawToSpreadsheet(object *SpreadsheetPushRequest) error {
	var vr sheets.ValueRange
	vr.Values = object.Values
	_, err := cli.sheet.Spreadsheets.Values.Append(object.SpreadsheetId, object.Range, &vr).
		ValueInputOption("RAW").Do()
	return errors.WrapAndReport(err, "append raw to google sheet")
}

func (cli *Clients) AppendUserEnterToSpreadsheet(object *SpreadsheetPushRequest) error {
	var vr sheets.ValueRange
	vr.Values = object.Values
	_, err := cli.sheet.Spreadsheets.Values.Append(object.SpreadsheetId, object.Range, &vr).
		ValueInputOption("USER_ENTERED").Do()
	return errors.WrapAndReport(err, "append user enter to google sheet")
}

func (cli *Clients) UpdateUserEnterToSpreadsheet(object *SpreadsheetPushRequest) error {
	var vr sheets.ValueRange
	vr.Values = object.Values
	_, err := cli.sheet.Spreadsheets.Values.Update(object.SpreadsheetId, object.Range, &vr).
		ValueInputOption("USER_ENTERED").Do()
	return errors.WrapAndReport(err, "update user enter to google sheet")
}

func (cli *Clients) UpdateRawToSpreadsheet(object *SpreadsheetPushRequest) error {
	var vr sheets.ValueRange
	vr.Values = object.Values
	_, err := cli.sheet.Spreadsheets.Values.Update(object.SpreadsheetId, object.Range, &vr).
		ValueInputOption("RAW").Do()
	return errors.WrapAndReport(err, "update raw to google sheet")
}

func oauth2Config(c *config.GoogleCredential, scopes ...string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURIs[0],
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  c.AuthURI,
			TokenURL: c.TokenURI,
		},
	}
}

func oauth2Token(token *config.Oauth2Token) *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}
}
