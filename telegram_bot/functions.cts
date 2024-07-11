import { configDotenv } from "dotenv";
import { createTransport } from "nodemailer";

configDotenv();

export const isMemberRegistered = async (id: number): Promise<boolean> => {
  const toret: boolean = await fetch(
    `http://${process.env.CRUD_ENDPOINT}/members/${id}`
  ).then((res: Response) => {
    return res.status === 200
  })
  return toret
};

export const sendMail = async (
  from: string,
  to: string,
  subject: string,
  html: string
) => {
  const transporter = createTransport({
    service: process.env.EMAIL_HOST,
    auth: {
      user: process.env.EMAIL_USER,
      pass: process.env.EMAIL_TOKEN,
    },
  });

  const mailOpts = {
    from: from,
    to: to,
    subject: subject,
    html: html,
  };

  console.log(`Sending mail to ${to}`);
  transporter.sendMail(mailOpts, (error, info) => {
    if (error) {
      console.error("Error in sending email:", error);
    } else {
      console.log(`Sent email to ${to}. Info: ${info}`);
    }
  });
};

export const generateToken = (): string => {
  const chars =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let result = "";
  for (let i = 0; i < 16; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return result;
};

export const generateMail = (token: string): string => {
  return `<!DOCTYPE html>
<html lang="it">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Verifica del Token</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            background-color: #f4f4f4;
            margin: 0;
            padding: 0;
        }
        .container {
            width: 100%;
            max-width: 600px;
            margin: 0 auto;
            background-color: #ffffff;
            padding: 20px;
            box-shadow: 0 0 10px rgba(0, 0, 0, 0.1);
        }
        .header {
            background-color: #007bff;
            color: #ffffff;
            text-align: center;
            padding: 10px 0;
        }
        .content {
            padding: 20px;
            text-align: center;
        }
        .token {
            font-size: 24px;
            font-weight: bold;
            color: #007bff;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Verifica del Token</h1>
        </div>
        <div class="content">
            <p>Ciao,</p>
            <p>Per completare la registrazione, per favore utilizza il seguente token di verifica:</p>
            <p class="token">${token}</p>
            <p>Se non hai richiesto questa email, per favore ignora questo messaggio.</p>
            <p>Grazie!</p>
        </div>
    </div>
</body>
</html>`;
};
