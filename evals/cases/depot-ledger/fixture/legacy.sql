PRAGMA user_version=1;
CREATE TABLE balances(site TEXT, sku TEXT, qty INTEGER NOT NULL, PRIMARY KEY(site,sku));
INSERT INTO balances VALUES ('north','tea',12),('south','tea',8),('north','rice',4);
