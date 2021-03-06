package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/netlify/gotrue/models"
)

// SignupParams are the parameters the Signup endpoint accepts
type SignupParams struct {
	Email    string                 `json:"email"`
	Password string                 `json:"password"`
	Data     map[string]interface{} `json:"data"`
	Provider string                 `json:"provider"`
	Code     string                 `json:"code"`
}

func (a *API) signupExternalProvider(ctx context.Context, w http.ResponseWriter, r *http.Request, params *SignupParams) error {
	provider, err := a.Provider(params.Provider)
	if err != nil {
		return badRequestError("Unsupported provider: %+v", err)
	}

	tok, err := provider.GetOAuthToken(ctx, params.Code)
	if err != nil {
		return internalServerError("Unable to exchange external code")
	}

	aud := a.requestAud(ctx, r)
	params.Email, err = provider.GetUserEmail(ctx, tok)
	if err != nil {
		return internalServerError("Error getting user email from external provider").WithInternalError(err)
	}

	if exists, err := a.db.IsDuplicatedEmail(params.Email, aud); exists {
		return badRequestError("User already exists")
	} else if err != nil {
		return internalServerError("Error checking for duplicate users").WithInternalError(err)
	}

	user, err := a.signupNewUser(params, aud)
	if err != nil {
		return err
	}

	if a.config.Mailer.Autoconfirm {
		user.Confirm()
	} else if user.ConfirmationSentAt.Add(a.config.Mailer.MaxFrequency).Before(time.Now()) {
		if err := a.mailer.ConfirmationMail(user); err != nil {
			return internalServerError("Error sending confirmation mail").WithInternalError(err)
		}
	}

	user.SetRole(a.config.JWT.DefaultGroupName)
	if err := a.db.UpdateUser(user); err != nil {
		return internalServerError("Error updating user in database").WithInternalError(err)
	}

	return sendJSON(w, http.StatusOK, user)
}

// Signup is the endpoint for registering a new user
func (a *API) Signup(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	params := &SignupParams{}

	jsonDecoder := json.NewDecoder(r.Body)
	err := jsonDecoder.Decode(params)
	if err != nil {
		return badRequestError("Could not read Signup params: %v", err)
	}

	if params.Provider != "" {
		if params.Code == "" {
			return unprocessableEntityError("Invalid code from OAuth provider")
		}
		return a.signupExternalProvider(ctx, w, r, params)
	}

	if params.Email == "" || params.Password == "" {
		return unprocessableEntityError("Signup requires a valid email and password")
	}

	aud := a.requestAud(ctx, r)

	user, err := a.db.FindUserByEmailAndAudience(params.Email, aud)
	if err != nil {
		if !models.IsNotFoundError(err) {
			return internalServerError("Database error finding user").WithInternalError(err)
		}

		user, err = a.signupNewUser(params, aud)
		if err != nil {
			return err
		}
	} else {
		user, err = a.confirmUser(user, params)
		if err != nil {
			return err
		}
	}

	if err = a.mailer.ValidateEmail(params.Email); err != nil {
		return unprocessableEntityError("Unable to validate email address: " + err.Error())
	}

	if a.config.Mailer.Autoconfirm {
		user.Confirm()
	} else if user.ConfirmationSentAt.Add(a.config.Mailer.MaxFrequency).Before(time.Now()) {
		if err := a.mailer.ConfirmationMail(user); err != nil {
			return internalServerError("Error sending confirmation mail").WithInternalError(err)
		}
	}

	user.SetRole(a.config.JWT.DefaultGroupName)
	if err = a.db.UpdateUser(user); err != nil {
		return internalServerError("Database error updating user").WithInternalError(err)
	}

	return sendJSON(w, http.StatusOK, user)
}

func (a *API) signupNewUser(params *SignupParams, aud string) (*models.User, error) {
	user, err := models.NewUser(params.Email, params.Password, aud, params.Data)
	if err != nil {
		return nil, internalServerError("Database error creating user").WithInternalError(err)
	}

	if params.Password == "" {
		user.EncryptedPassword = ""
	}

	if err := a.db.CreateUser(user); err != nil {
		return nil, internalServerError("Database error saving new user").WithInternalError(err)
	}

	return user, nil
}

func (a *API) confirmUser(user *models.User, params *SignupParams) (*models.User, error) {
	if user.IsRegistered() {
		return nil, badRequestError("A user with this email address has already been registered")
	}

	user.UpdateUserMetaData(params.Data)
	user.GenerateConfirmationToken()

	if err := a.db.UpdateUser(user); err != nil {
		return nil, internalServerError("Database error updating user").WithInternalError(err)
	}

	return user, nil
}
