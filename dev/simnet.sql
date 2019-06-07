-- This resets the dcrdata_simnet database.

DROP DATABASE IF EXISTS dcrdata_simnet;
DROP USER IF exists dcrdata_simnet_stooge;

CREATE USER dcrdata_simnet_stooge PASSWORD 'pass';
CREATE DATABASE dcrdata_simnet OWNER dcrdata_simnet_stooge;
