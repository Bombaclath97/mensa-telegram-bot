"use strict";
var __awaiter = (this && this.__awaiter) || function (thisArg, _arguments, P, generator) {
    function adopt(value) { return value instanceof P ? value : new P(function (resolve) { resolve(value); }); }
    return new (P || (P = Promise))(function (resolve, reject) {
        function fulfilled(value) { try { step(generator.next(value)); } catch (e) { reject(e); } }
        function rejected(value) { try { step(generator["throw"](value)); } catch (e) { reject(e); } }
        function step(result) { result.done ? resolve(result.value) : adopt(result.value).then(fulfilled, rejected); }
        step((generator = generator.apply(thisArg, _arguments || [])).next());
    });
};
var __generator = (this && this.__generator) || function (thisArg, body) {
    var _ = { label: 0, sent: function() { if (t[0] & 1) throw t[1]; return t[1]; }, trys: [], ops: [] }, f, y, t, g;
    return g = { next: verb(0), "throw": verb(1), "return": verb(2) }, typeof Symbol === "function" && (g[Symbol.iterator] = function() { return this; }), g;
    function verb(n) { return function (v) { return step([n, v]); }; }
    function step(op) {
        if (f) throw new TypeError("Generator is already executing.");
        while (g && (g = 0, op[0] && (_ = 0)), _) try {
            if (f = 1, y && (t = op[0] & 2 ? y["return"] : op[0] ? y["throw"] || ((t = y["return"]) && t.call(y), 0) : y.next) && !(t = t.call(y, op[1])).done) return t;
            if (y = 0, t) op = [op[0] & 2, t.value];
            switch (op[0]) {
                case 0: case 1: t = op; break;
                case 4: _.label++; return { value: op[1], done: false };
                case 5: _.label++; y = op[1]; op = [0]; continue;
                case 7: op = _.ops.pop(); _.trys.pop(); continue;
                default:
                    if (!(t = _.trys, t = t.length > 0 && t[t.length - 1]) && (op[0] === 6 || op[0] === 2)) { _ = 0; continue; }
                    if (op[0] === 3 && (!t || (op[1] > t[0] && op[1] < t[3]))) { _.label = op[1]; break; }
                    if (op[0] === 6 && _.label < t[1]) { _.label = t[1]; t = op; break; }
                    if (t && _.label < t[2]) { _.label = t[2]; _.ops.push(op); break; }
                    if (t[2]) _.ops.pop();
                    _.trys.pop(); continue;
            }
            op = body.call(thisArg, _);
        } catch (e) { op = [6, e]; y = 0; } finally { f = t = 0; }
        if (op[0] & 5) throw op[1]; return { value: op[0] ? op[1] : void 0, done: true };
    }
};
Object.defineProperty(exports, "__esModule", { value: true });
var dotenv_1 = require("dotenv");
var TelegramBot = require("node-telegram-bot-api");
var mapify_ts_1 = require("mapify-ts");
var functions_cjs_1 = require("./functions.cjs");
(0, dotenv_1.configDotenv)();
var BOT_TOKEN = process.env.BOT_TOKEN;
var bot = new TelegramBot(BOT_TOKEN, {
    polling: true,
});
var botCommands = [
    { command: "start", description: "start" },
    { command: "profilo", description: "profile" },
];
bot.setMyCommands(botCommands);
var requestsToApprove = new Map();
bot.onText(/\/start/, startRoutine);
bot.onText(/\/profilo/, profileRoutine);
bot.on("message", answerUserState);
bot.on("chat_join_request", onChatJoinRequest);
function startRoutine(msg, match) {
    return __awaiter(this, void 0, void 0, function () {
        var id;
        return __generator(this, function (_a) {
            if (!(0, functions_cjs_1.isMemberRegistered)(msg.from.id)) {
                bot.sendMessage(msg.chat.id, "Iscriviti! Usa /profile");
            }
            else {
                bot.sendMessage(msg.chat.id, "Sei già registrato!");
                id = requestsToApprove.get(msg.from.id);
                bot.approveChatJoinRequest(id, msg.from.id);
            }
            return [2 /*return*/];
        });
    });
}
function profileRoutine(msg, match) {
    return __awaiter(this, void 0, void 0, function () {
        var chatId;
        var _this = this;
        return __generator(this, function (_a) {
            switch (_a.label) {
                case 0:
                    chatId = msg.chat.id;
                    return [4 /*yield*/, (0, functions_cjs_1.isMemberRegistered)(msg.from.id).then(function (registered) { return __awaiter(_this, void 0, void 0, function () {
                            var response, jsonResponse, user;
                            return __generator(this, function (_a) {
                                switch (_a.label) {
                                    case 0:
                                        if (!!registered) return [3 /*break*/, 1];
                                        resetUserState(chatId);
                                        bot.sendMessage(chatId, "Non risulti ancora registrato. Creiamo il tuo profilo! Puoi inviarmi il tuo nome, così come risulta in Area32?");
                                        users[chatId].state = "ASK_FIRST_NAME";
                                        return [3 /*break*/, 4];
                                    case 1:
                                        if (!(!users[chatId] || !users[chatId].mensaNumber)) return [3 /*break*/, 3];
                                        return [4 /*yield*/, fetch("http://".concat(process.env.CRUD_ENDPOINT, "/members/").concat(msg.from.id)).then(function (res) {
                                                return res.text();
                                            })];
                                    case 2:
                                        response = _a.sent();
                                        jsonResponse = JSON.parse(response);
                                        console.log(jsonResponse);
                                        users[chatId] = {
                                            state: null,
                                            firstName: jsonResponse["firstName"],
                                            lastName: jsonResponse["lastName"],
                                            email: jsonResponse["mensaEmail"],
                                            mensaNumber: jsonResponse["mensaNumber"],
                                            membershipEndDate: jsonResponse["membershipEndDate"],
                                            token: null,
                                        };
                                        _a.label = 3;
                                    case 3:
                                        user = users[chatId];
                                        bot.sendMessage(chatId, "Ciao ".concat(user.firstName, " ").concat(user.lastName, "! La tua email: ").concat(user.email, ". La tua tessera: ").concat(user.mensaNumber, " scadr\u00E0 il ").concat(user.membershipEndDate));
                                        _a.label = 4;
                                    case 4: return [2 /*return*/];
                                }
                            });
                        }); })];
                case 1:
                    _a.sent();
                    return [2 /*return*/];
            }
        });
    });
}
function answerUserState(msg) {
    return __awaiter(this, void 0, void 0, function () {
        var chatId, text, user, _a, html, response_post;
        var _b;
        return __generator(this, function (_c) {
            switch (_c.label) {
                case 0:
                    chatId = msg.chat.id;
                    text = (_b = msg.text) === null || _b === void 0 ? void 0 : _b.trim();
                    if (!text || !users[chatId] || !users[chatId].state) {
                        return [2 /*return*/];
                    }
                    user = users[chatId];
                    console.log(user.state);
                    _a = user.state;
                    switch (_a) {
                        case "ASK_FIRST_NAME": return [3 /*break*/, 1];
                        case "ASK_LAST_NAME": return [3 /*break*/, 2];
                        case "CONFIRM_EMAIL": return [3 /*break*/, 3];
                        case "ASK_EMAIL": return [3 /*break*/, 4];
                        case "ASK_TOKEN": return [3 /*break*/, 5];
                        case "DONE": return [3 /*break*/, 9];
                    }
                    return [3 /*break*/, 10];
                case 1:
                    user.firstName = text;
                    bot.sendMessage(chatId, "Grazie. E il tuo cognome?");
                    user.state = "ASK_LAST_NAME";
                    return [3 /*break*/, 11];
                case 2:
                    user.lastName = text;
                    bot.sendMessage(chatId, "Perfetto. La tua mail mensa.it dovrebbe essere ".concat(user.firstName.toLowerCase(), ".").concat(user.lastName.toLowerCase(), "@mensa.it\n\u00C8 corretto? Rispondi \"S\u00EC/No\""));
                    user.state = "CONFIRM_EMAIL";
                    return [3 /*break*/, 11];
                case 3:
                    if (text.toLowerCase() === "sì" || text.toLowerCase() === "si") {
                        if (!user.email) {
                            user.email = "".concat(user.firstName.toLowerCase(), ".").concat(user.lastName.toLowerCase(), "@mensa.it");
                        }
                        user.token = (0, functions_cjs_1.generateToken)();
                        html = (0, functions_cjs_1.generateMail)(user.token);
                        (0, functions_cjs_1.sendMail)(process.env.MAIL_USER, user.email, "Mensa Italia - Verifica profilo su bot Telegram", html);
                        bot.sendMessage(chatId, "Ho mandato un token a ".concat(user.email, ". Scrivimelo!"));
                        user.state = "ASK_TOKEN";
                    }
                    else if (text.toLowerCase() === "no") {
                        bot.sendMessage(chatId, "Ok. Per favore scrivi la tua mail con dominio @mensa.it.");
                        user.state = "ASK_EMAIL";
                    }
                    else {
                        bot.sendMessage(chatId, "Non ho capito, rispondi \"s\u00EC\" o \"no\", per favore. La tua mail \u00E8 ".concat(user.firstName.toLowerCase(), ".").concat(user.lastName.toLowerCase(), "@mensa.it, corretto?"));
                    }
                    return [3 /*break*/, 11];
                case 4:
                    if (text.endsWith("@mensa.it")) {
                        user.email = text;
                        bot.sendMessage(chatId, "Perfetto. La tua mail mensa.it dovrebbe essere ".concat(user.email, "\n\u00C8 corretto? Rispondi \"S\u00EC/No\""));
                        user.state = "CONFIRM_EMAIL";
                    }
                    else {
                        bot.sendMessage(chatId, "Per favore manda un indirizzo mail valido con dominio @mensa.it!");
                    }
                    return [3 /*break*/, 11];
                case 5:
                    if (!(text.trim() === user.token)) return [3 /*break*/, 7];
                    bot.sendMessage(chatId, "Grazie!");
                    return [4 /*yield*/, fetch("http://".concat(process.env.CRUD_ENDPOINT, "/members"), {
                            method: "POST",
                            body: "{\n                      \"telegramId\": ".concat(msg.from.id, ",\n                      \"mensaEmail\": \"").concat(user.email, "\",\n                      \"mensaNumber\": 1214,\n                      \"membershipEndDate\": \"2024-05-07T00:00:00Z\",\n                      \"firstName\": \"").concat(user.firstName, "\",\n                      \"lastName\": \"").concat(user.lastName, "\"\n                  }"),
                        })];
                case 6:
                    response_post = _c.sent();
                    user.state = "DONE";
                    return [3 /*break*/, 8];
                case 7:
                    bot.sendMessage(chatId, "Token errato. Per favore riprova!");
                    _c.label = 8;
                case 8: return [3 /*break*/, 11];
                case 9: return [3 /*break*/, 11];
                case 10: return [3 /*break*/, 11];
                case 11: return [2 /*return*/];
            }
        });
    });
}
function onChatJoinRequest(joinRequest) {
    var fromId = joinRequest.from.id;
    if (!(0, functions_cjs_1.isMemberRegistered)(fromId)) {
        bot.sendMessage(fromId, "Usa /start");
        requestsToApprove.set(fromId, joinRequest.chat.id);
        var serializedMap = mapify_ts_1.default.serialize(requestsToApprove);
    }
    else {
        bot.approveChatJoinRequest(joinRequest.chat.id, fromId);
    }
}
var users = {};
function resetUserState(userId) {
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
