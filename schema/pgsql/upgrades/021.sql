ALTER TABLE source ADD COLUMN listener_password_hash text;
-- php > $listener_password_hash = password_hash("correct horse battery staple", PASSWORD_DEFAULT));
UPDATE source SET listener_password_hash = '$2y$10$QU8bJ7cpW1SmoVQ/RndX5O2J5L1PJF7NZ2dlIW7Rv3zUEcbUFg3z2';
ALTER TABLE source
    ALTER COLUMN listener_password_hash SET NOT NULL,
    ADD CHECK (listener_password_hash LIKE '$2y$%');
