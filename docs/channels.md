# Chat channels: Telegram and Discord

A **channel** is a two-way link between you and onett through a chat app. From your phone you can
get alerts, chat with the orchestrator (the agent that creates and manages tasks for you), and
approve or reject merges with a button. A channel is different from a notifier: a notifier only
sends messages out, a channel also reads what you send.

Onett supports two channels, Telegram and Discord. Both connect out from your machine, so you
need no public URL and no open port.

## How both channels behave

- **Only allowlisted users are obeyed.** `allowed_user_ids` lists who may talk to onett. Anyone
  else is ignored without a reply. An allowed user can approve merges, so keep the list short.
- **Direct messages only.** Group chats and server channels are ignored.
- **One conversation per chat.** Each direct-message chat gets its own orchestrator conversation.
  The link is saved in the channel's state file, so a restart does not lose it.
- **Approvals arrive with buttons.** When a task needs your decision, the bot sends the request
  with Approve and Reject buttons. The approval is pinned to one commit, as in the web UI.
- **Tokens stay secret.** The config holds only the *name* of an environment variable, never the
  token. Onett never logs the token.

## Keep the bot token in secrets.env

Put the token in `~/.config/onett/secrets.env`, next to the config. Onett reads that file when
`onett serve` starts (see [step 12 of getting started](getting-started.md#12-keep-secrets-out-of-the-config)):

```sh
mkdir -p ~/.config/onett
touch ~/.config/onett/secrets.env
chmod 600 ~/.config/onett/secrets.env
code ~/.config/onett/secrets.env    # add TELEGRAM_BOT_TOKEN=... and/or DISCORD_BOT_TOKEN=...
```

Restart `onett serve` after you change the file.

## Config

Channels are a top-level `channels:` list in `onett.yaml`. Only the first entry of each type is used.

```yaml
channels:
  - type: telegram
    options:
      bot_token_env: TELEGRAM_BOT_TOKEN        # required; the variable's name, not the token
      allowed_user_ids: [123456789]            # required; numeric Telegram user IDs
      # state_file: /path/telegram.json        # default <data_dir>/channels/telegram.json
  - type: discord
    options:
      bot_token_env: DISCORD_BOT_TOKEN         # required
      allowed_user_ids: ["123456789012345678"] # required; Discord user IDs as quoted strings
      # state_file: /path/discord.json         # default <data_dir>/channels/discord.json
```

Unknown option names are errors. Write Discord IDs as quoted strings: they are 18 or 19 digits, and
an unquoted number can lose digits.

## Telegram

1. In Telegram, message **@BotFather** and send `/newbot`. Follow the prompts. BotFather replies
   with the bot token. Save it as `TELEGRAM_BOT_TOKEN` in `secrets.env`.
2. Find your numeric user ID: message **@userinfobot** and it replies with your ID. Put it in
   `allowed_user_ids`.
3. Add the `telegram` entry above to `onett.yaml` and restart `onett serve`.
4. Open your new bot in Telegram and send it a message (`/start` works). A bot cannot start a
   chat with you, so it can only send you alerts and approvals after you have written to it once.

The bot uses long polling: it asks Telegram for new messages over an outgoing HTTPS request.

### Telegram notifiers

If you also have a `telegram` entry under `notify:`, onett turns on its `skip_approvals` option
while a telegram channel is configured. Approval requests then reach you only through the bot, with
buttons, and are not delivered twice. Other notifications still go out as before. A Discord
channel does not change any notifier.

## Discord

1. Open the [Discord Developer Portal](https://discord.com/developers/applications), choose
   **New Application**, then open **Bot** and add a bot. Choose **Reset Token** to reveal the
   token and save it as `DISCORD_BOT_TOKEN` in `secrets.env`.
2. The bot needs only the `DIRECT_MESSAGES` gateway intent, which onett always requests and which
   is not privileged. Leave **Message Content Intent** off: it is not needed, because bots receive
   the text of direct messages without it.
3. Let the bot share a server with you. Discord only lets a bot message a user it shares a server
   with. Under **OAuth2 → URL Generator**, tick the `bot` scope, open the link it produces, and
   add the bot to a server you are in (a small private server is fine). No permissions are needed.
4. Find your user ID: in Discord, turn on **Settings → Advanced → Developer Mode**, then
   right-click your name and choose **Copy User ID**. Put it, in quotes, in `allowed_user_ids`.
5. Add the `discord` entry above to `onett.yaml` and restart `onett serve`.
6. Direct-message the bot. Approvals work even before you write first: onett opens a DM with each
   allowed user when it has one to send.

The bot connects to the Discord Gateway, a websocket that onett opens outward, so you need no
public URL and no interactions endpoint URL in the portal. Messages in server channels are
ignored; only direct messages count.

## Troubleshooting

- **`channel telegram: environment variable "X" (bot_token_env) is empty`** (or `channel discord:`):
  the variable named in `bot_token_env` is not set. Add it to `~/.config/onett/secrets.env` and
  restart. Onett refuses to start until it is set.
- **`options.allowed_user_ids: must list at least one ... user ID`**: the allowlist is empty or
  missing. Telegram IDs must be whole numbers; Discord IDs must be digits in quotes.
- **The bot does not answer.** Check that your ID is in `allowed_user_ids` (a wrong ID is ignored
  without any reply), that you are messaging it directly and not in a group or server channel,
  and, for Telegram, that you sent it a first message. Then look at the `onett serve` log for
  `telegram channel` or `discord channel` lines.
- **`discord channel stopped ... gateway refused the bot`**: Discord rejected the bot, usually a
  wrong or reset token. Make a new token in the portal and update `secrets.env`.
- **A bot failure does not stop onett.** If a channel cannot start or stops, the daemon logs the
  error and keeps running.
- **Where is the state?** In the `state_file`, by default `<data_dir>/channels/telegram.json` and
  `<data_dir>/channels/discord.json`. They hold conversation links (and Telegram's poll position),
  never the token. Delete one to make the bot start fresh conversations.
