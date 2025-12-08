# Mensa Telegram Bot - Agent Context

## Project Overview

This is a Telegram bot written in Go that facilitates membership verification for Mensa Italy. The bot conducts interactive conversations with users to collect their email address (restricted to @mensa.it domain), membership number, and full name. It integrates with an external Mensa application API to verify membership credentials and requires user confirmation via push notifications before completing the verification process.

## Core Functionality

The bot implements a stateful conversation engine that guides users through a multi-step verification process:

1. **Email Collection**: Prompts user for email address, validates @mensa.it domain
2. **Membership Verification**: Collects membership number and sends verification request to Mensa app
3. **Notification Confirmation**: Waits for user to approve/reject via Mensa app notification
4. **Profile Completion**: Collects full name and confirms profile
5. **Success/Failure**: Ends conversation with appropriate status

## Architecture

### Key Components

- **Conversation Engine** (`convo/`): Manages conversation state and step transitions
  - `engine.go`: Core engine with TTL-based conversation management
  - `step.go`: Step interface definition
  - `steps/`: Individual step implementations
  - `convo_context.go`: Data model for conversation state
  - `steps_registry.go`: Registry for step factories

- **Context Management** (`convo/`): Centralized access to conversation contexts
  - `store.go`: GORM-based SQLite store implementation
  - `context_manager.go`: Global functions for context access (GetConvoContext, SaveConvoContext, etc.)

- **REST Integration** (`rest_client/`, `handlers/`, `routers/`):
  - `mensa_app_rest_client.go`: Client for communicating with external Mensa API
  - `callback_handler.go`: Handles callback responses from Mensa app
  - `notification_callback.go`: HTTP router setup

- **Utilities** (`utils/`):
  - `generators.go`: Generates secure callback URLs

### Data Flow

```
User Message → Engine.Process() → Global Context Access → Current Step.Handle() → Next Step
                                      ↓
                            REST Client → Mensa API
                                      ↓
                            Callback URL ← Mensa App
                                      ↓
                            Global Context Update → Database
```

### Technologies

- **Language**: Go 1.25.4
- **Bot Framework**: github.com/go-telegram/bot
- **Database**: SQLite with GORM ORM
- **HTTP Router**: go-chi/chi
- **Custom Utils**: github.com/Bombaclath97/bomba-go-utils

## Environment Variables

- `SELF_BASE_URL`: Base URL for callback endpoints (default: localhost:3000)
- `MENSA_APP_ENDPOINT`: External Mensa API endpoint (default: localhost:3001)
- `MENSA_APP_API_TOKEN`: Bearer token for Mensa API authentication
- `CONVERSATION_TTL`: Conversation timeout in minutes (default: 30)

## Database Schema

The `ConvoContext` model (using GORM) includes:
- UserID (int64): Telegram user identifier
- Email (string): User's @mensa.it email
- Membership (string): Mensa membership number
- FullName (string): User's full name
- CurrentStep (AvailableSteps): Current conversation step
- LastInteraction (time.Time): Timestamp of last user interaction
- Input (string): Current user input
- CallbackUrl (string): Generated callback endpoint
- LockedInteraction (bool): Whether interaction is locked pending callback

## Centralized Context Access

ConvoContext is now accessible globally throughout the application:

```go
// Initialize global store at startup
convo.InitGlobalStore(store)

// Access contexts anywhere
ctx, err := convo.GetConvoContext(userID)
convo.SaveConvoContext(ctx)
convo.ClearConvoContext(userID)

// Update pattern
convo.UpdateConvoContext(userID, func(ctx *convo.ConvoContext) error {
    ctx.LockedInteraction = false
    ctx.CurrentStep = convo.AskFullName
    return nil
})
```

This eliminates the need to pass contexts through function parameters and allows any component (like HTTP handlers) to access conversation state directly.

## Step Registry

Available conversation steps:
- AskEmail
- AskMembership
- AskFullName
- ConfirmProfile
- EndSuccess
- EndFailed
- EndCancelled
- EndTimeout

## API Integration

### Outbound: Membership Verification
```
POST {MENSA_APP_ENDPOINT}/request
Headers: Authorization: Bearer {MENSA_APP_API_TOKEN}
Body: email={email}&member_id={membership}&callme_url={SELF_BASE_URL}/{userId}_{token}
```

### Inbound: Callback Handler
```
POST /callback/{userId}_{token}
Body: {"accepted": true/false}
```

## Security Considerations

- Email validation restricted to @mensa.it domain
- Secure random token generation for callback URLs
- Bearer token authentication for external API
- Conversation timeouts to prevent stale sessions
- Input sanitization and validation

## Development Notes

- Global context access simplifies component interactions but requires careful management of concurrent access
- Uses println() for messaging (likely placeholders for actual Telegram message sending)
- Registry-based step system allows for easy extension
- SQLite with GORM provides solid persistence without external dependencies
- Engine is now stateless, relying on global context management

## Deployment

The application requires:
- Go runtime environment
- Network access to Telegram Bot API
- Network access to Mensa application API
- SQLite database file (auto-created by GORM)

No Docker configuration or build scripts are included in the current codebase.
