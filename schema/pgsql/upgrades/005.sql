CREATE TYPE boolenum AS ENUM ( 'n', 'y' );
ALTER TABLE rule ADD COLUMN is_active boolenum NOT NULL DEFAULT 'y';
