import { configDotenv } from "dotenv";
import TelegramBot = require("node-telegram-bot-api");
import MapifyTs from "mapify-ts";
import {
  isMemberRegistered,
  sendMail,
  generateToken,
  generateMail,
} from "./functions.cjs";

configDotenv();
const BOT_TOKEN = process.env.BOT_TOKEN;

const bot = new TelegramBot(BOT_TOKEN, {
  polling: true,
});

const botCommands: TelegramBot.BotCommand[] = [
  { command: "start", description: "start" },
  { command: "profilo", description: "profile" },
];

bot.setMyCommands(botCommands);

let requestsToApprove = new Map<number, number>();

bot.onText(/\/start/, startRoutine);
bot.onText(/\/profilo/, profileRoutine);
bot.on("message", answerUserState);
bot.on("chat_join_request", onChatJoinRequest);

async function startRoutine(msg: TelegramBot.Message, match: RegExpExecArray) {
  if (!isMemberRegistered(msg.from.id)) {
    bot.sendMessage(msg.chat.id, "Iscriviti! Usa /profile");
  } else {
    bot.sendMessage(msg.chat.id, "Sei già registrato!");

    let id = requestsToApprove.get(msg.from.id);
    bot.approveChatJoinRequest(id, msg.from.id);
  }
}

async function profileRoutine(
  msg: TelegramBot.Message,
  match: RegExpExecArray
) {
  const chatId = msg.chat.id;
  await isMemberRegistered(msg.from.id).then(async (registered: boolean) => {
    if (!registered) {
      resetUserState(chatId);
      bot.sendMessage(
        chatId,
        "Non risulti ancora registrato. Creiamo il tuo profilo! Puoi inviarmi il tuo nome, così come risulta in Area32?"
      );
      users[chatId].state = "ASK_FIRST_NAME";
    } else {
      if (!users[chatId] || !users[chatId].mensaNumber) {
        const response: string = await fetch(
          `http://${process.env.CRUD_ENDPOINT}/members/${msg.from.id}`
        ).then((res) => {
          return res.text();
        });
        const jsonResponse = JSON.parse(response);
        console.log(jsonResponse)
        users[chatId] = {
          state: null,
          firstName: jsonResponse["firstName"],
          lastName: jsonResponse["lastName"],
          email: jsonResponse["mensaEmail"],
          mensaNumber: jsonResponse["mensaNumber"],
          membershipEndDate: jsonResponse["membershipEndDate"],
          token: null,
        };
      }
      let user = users[chatId]
      bot.sendMessage(chatId, `Ciao ${user.firstName} ${user.lastName}! La tua email: ${user.email}. La tua tessera: ${user.mensaNumber} scadrà il ${user.membershipEndDate}`)
    }
  });
}

async function answerUserState(msg: TelegramBot.Message) {
  const chatId = msg.chat.id;
  const text = msg.text?.trim();

  if (!text || !users[chatId] || !users[chatId].state) {
    return;
  }

  const user = users[chatId];

  console.log(user.state);

  switch (user.state) {
    case "ASK_FIRST_NAME":
      user.firstName = text;
      bot.sendMessage(chatId, "Grazie. E il tuo cognome?");
      user.state = "ASK_LAST_NAME";
      break;

    case "ASK_LAST_NAME":
      user.lastName = text;
      bot.sendMessage(
        chatId,
        `Perfetto. La tua mail mensa.it dovrebbe essere ${user.firstName.toLowerCase()}.${user.lastName.toLowerCase()}@mensa.it\nÈ corretto? Rispondi "Sì/No"`
      );
      user.state = "CONFIRM_EMAIL";
      break;

    case "CONFIRM_EMAIL":
      if (text.toLowerCase() === "sì" || text.toLowerCase() === "si") {
        if (!user.email) {
          user.email = `${user.firstName.toLowerCase()}.${user.lastName.toLowerCase()}@mensa.it`;
        }
        user.token = generateToken();
        let html: string = generateMail(user.token);
        sendMail(
          process.env.MAIL_USER,
          user.email,
          "Mensa Italia - Verifica profilo su bot Telegram",
          html
        );
        bot.sendMessage(
          chatId,
          `Ho mandato un token a ${user.email}. Scrivimelo!`
        );
        user.state = "ASK_TOKEN";
      } else if (text.toLowerCase() === "no") {
        bot.sendMessage(
          chatId,
          "Ok. Per favore scrivi la tua mail con dominio @mensa.it."
        );
        user.state = "ASK_EMAIL";
      } else {
        bot.sendMessage(
          chatId,
          `Non ho capito, rispondi "sì" o "no", per favore. La tua mail è ${user.firstName.toLowerCase()}.${user.lastName.toLowerCase()}@mensa.it, corretto?`
        );
      }
      break;

    case "ASK_EMAIL":
      if (text.endsWith("@mensa.it")) {
        user.email = text;
        bot.sendMessage(
          chatId,
          `Perfetto. La tua mail mensa.it dovrebbe essere ${user.email}\nÈ corretto? Rispondi "Sì/No"`
        );
        user.state = "CONFIRM_EMAIL";
      } else {
        bot.sendMessage(
          chatId,
          "Per favore manda un indirizzo mail valido con dominio @mensa.it!"
        );
      }
      break;

    case "ASK_TOKEN":
      if (text.trim() === user.token) {
        bot.sendMessage(chatId, "Grazie!");

        const response_post = await fetch(
          `http://${process.env.CRUD_ENDPOINT}/members`,
          {
            method: "POST",
            body: `{
                      "telegramId": ${msg.from.id},
                      "mensaEmail": "${user.email}",
                      "mensaNumber": 1214,
                      "membershipEndDate": "2024-05-07T00:00:00Z",
                      "firstName": "${user.firstName}",
                      "lastName": "${user.lastName}"
                  }`,
          }
        );
        user.state = "DONE";
      } else {
        bot.sendMessage(chatId, "Token errato. Per favore riprova!");
      }
      break;
    case "DONE":
      break;
    default:
      break;
  }
}

function onChatJoinRequest(joinRequest: TelegramBot.ChatJoinRequest) {
  const fromId = joinRequest.from.id;
  if (!isMemberRegistered(fromId)) {
    bot.sendMessage(fromId, "Usa /start");
    requestsToApprove.set(fromId, joinRequest.chat.id);
    const serializedMap = MapifyTs.serialize(requestsToApprove);
  } else {
    bot.approveChatJoinRequest(joinRequest.chat.id, fromId);
  }
}

interface UserState {
  state: string | null;
  firstName: string | null;
  lastName: string | null;
  email: string | null;
  mensaNumber: number | null;
  membershipEndDate: string | null;
  token: string | null;
}

const users: Record<number, UserState> = {};

function resetUserState(userId: number): void {
  users[userId] = {
    state: null,
    firstName: null,
    lastName: null,
    email: null,
    mensaNumber: null,
membershipEndDate: null,
    token: null,
  };
}
