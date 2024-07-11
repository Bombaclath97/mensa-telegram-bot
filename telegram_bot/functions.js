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
exports.generateMail = exports.generateToken = exports.sendMail = exports.isMemberRegistered = void 0;
var dotenv_1 = require("dotenv");
var nodemailer_1 = require("nodemailer");
(0, dotenv_1.configDotenv)();
var isMemberRegistered = function (id) { return __awaiter(void 0, void 0, void 0, function () {
    var response_get;
    return __generator(this, function (_a) {
        switch (_a.label) {
            case 0: return [4 /*yield*/, fetch("http://".concat(process.env.CRUD_ENDPOINT, "/members/").concat(id))];
            case 1:
                response_get = _a.sent();
                return [2 /*return*/, response_get.ok];
        }
    });
}); };
exports.isMemberRegistered = isMemberRegistered;
var sendMail = function (from, to, subject, html) { return __awaiter(void 0, void 0, void 0, function () {
    var transporter, mailOpts;
    return __generator(this, function (_a) {
        transporter = (0, nodemailer_1.createTransport)({
            service: process.env.EMAIL_HOST,
            auth: {
                user: process.env.EMAIL_USER,
                pass: process.env.EMAIL_TOKEN,
            },
        });
        mailOpts = {
            from: from,
            to: to,
            subject: subject,
            html: html,
        };
        console.log("Sending mail to ".concat(to));
        transporter.sendMail(mailOpts, function (error, info) {
            if (error) {
                console.error("Error in sending email:", error);
            }
            else {
                console.log("Sent email");
            }
        });
        return [2 /*return*/];
    });
}); };
exports.sendMail = sendMail;
var generateToken = function () {
    var chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
    var result = "";
    for (var i = 0; i < 16; i++) {
        result += chars.charAt(Math.floor(Math.random() * chars.length));
    }
    return result;
};
exports.generateToken = generateToken;
var generateMail = function (token) {
    return "<!DOCTYPE html>\n<html lang=\"it\">\n<head>\n    <meta charset=\"UTF-8\">\n    <meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">\n    <title>Verifica del Token</title>\n    <style>\n        body {\n            font-family: Arial, sans-serif;\n            background-color: #f4f4f4;\n            margin: 0;\n            padding: 0;\n        }\n        .container {\n            width: 100%;\n            max-width: 600px;\n            margin: 0 auto;\n            background-color: #ffffff;\n            padding: 20px;\n            box-shadow: 0 0 10px rgba(0, 0, 0, 0.1);\n        }\n        .header {\n            background-color: #007bff;\n            color: #ffffff;\n            text-align: center;\n            padding: 10px 0;\n        }\n        .content {\n            padding: 20px;\n            text-align: center;\n        }\n        .token {\n            font-size: 24px;\n            font-weight: bold;\n            color: #007bff;\n        }\n    </style>\n</head>\n<body>\n    <div class=\"container\">\n        <div class=\"header\">\n            <h1>Verifica del Token</h1>\n        </div>\n        <div class=\"content\">\n            <p>Ciao,</p>\n            <p>Per completare la registrazione, per favore utilizza il seguente token di verifica:</p>\n            <p class=\"token\">".concat(token, "</p>\n            <p>Se non hai richiesto questa email, per favore ignora questo messaggio.</p>\n            <p>Grazie!</p>\n        </div>\n    </div>\n</body>\n</html>");
};
exports.generateMail = generateMail;
