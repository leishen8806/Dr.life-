-- +goose Up
-- A platform must always retain an enabled super administrator.  This database
-- guard complements application checks so a direct SQL update cannot lock out
-- every administrator.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION iam.prevent_last_active_super()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.role = 'super' AND OLD.status = 'active'
       AND (TG_OP = 'DELETE' OR NEW.role <> 'super' OR NEW.status <> 'active')
       AND NOT EXISTS (
           SELECT 1
           FROM iam.admin_user
           WHERE role = 'super'
             AND status = 'active'
             AND id <> OLD.id
       )
    THEN
        RAISE EXCEPTION 'cannot remove the last active super administrator'
            USING ERRCODE = 'P0001';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER trg_admin_user_last_active_super
BEFORE DELETE OR UPDATE OF role, status ON iam.admin_user
FOR EACH ROW EXECUTE FUNCTION iam.prevent_last_active_super();

-- +goose Down
DROP TRIGGER trg_admin_user_last_active_super ON iam.admin_user;
DROP FUNCTION iam.prevent_last_active_super();
