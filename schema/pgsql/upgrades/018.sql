CREATE TABLE available_channel_type (
      type text NOT NULL,
      name text NOT NULL,
      version text NOT NULL,
      author text NOT NULL,
      config_attrs text NOT NULL,

      CONSTRAINT pk_available_channel_type PRIMARY KEY (type)
);

INSERT INTO available_channel_type (type, name, version, author, config_attrs)
    VALUES ('email', 'Email', '0.0.0', 'Icinga GmbH', ''), ('rocketchat', 'Rocket.Chat', '0.0.0', 'Icinga GmbH', '');

ALTER TABLE channel
    ADD CONSTRAINT channel_type_fkey FOREIGN KEY (type) REFERENCES available_channel_type(type);
