-- Add new captcha configuration fields
INSERT INTO `system` (`category`, `key`, `value`, `type`, `desc`) VALUES
    ('verify', 'CaptchaType', 'local', 'string', 'Captcha type: local or turnstile'),
    ('verify', 'EnableUserLoginCaptcha', 'false', 'bool', 'Enable captcha for user login'),
    ('verify', 'EnableUserRegisterCaptcha', 'false', 'bool', 'Enable captcha for user registration'),
    ('verify', 'EnableAdminLoginCaptcha', 'false', 'bool', 'Enable captcha for admin login'),
    ('verify', 'EnableUserResetPasswordCaptcha', 'false', 'bool', 'Enable captcha for user reset password')
ON DUPLICATE KEY UPDATE
    `value` = VALUES(`value`),
    `desc` = VALUES(`desc`);

-- Remove old verify configuration fields
DELETE FROM `system` WHERE `category` = 'verify' AND `key` IN (
    'EnableLoginVerify',
    'EnableRegisterVerify',
    'EnableResetPasswordVerify'
);
