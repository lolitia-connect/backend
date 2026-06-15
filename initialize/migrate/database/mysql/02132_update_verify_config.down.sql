-- Rollback: restore old verify configuration fields
INSERT INTO `system` (`category`, `key`, `value`, `type`, `desc`) VALUES
    ('verify', 'EnableLoginVerify', 'false', 'bool', 'is enable login verify'),
    ('verify', 'EnableRegisterVerify', 'false', 'bool', 'is enable register verify'),
    ('verify', 'EnableResetPasswordVerify', 'false', 'bool', 'is enable reset password verify')
ON DUPLICATE KEY UPDATE
    `value` = VALUES(`value`),
    `desc` = VALUES(`desc`);

-- Remove new captcha configuration fields
DELETE FROM `system` WHERE `category` = 'verify' AND `key` IN (
    'CaptchaType',
    'EnableUserLoginCaptcha',
    'EnableUserRegisterCaptcha',
    'EnableAdminLoginCaptcha',
    'EnableUserResetPasswordCaptcha'
);
