FROM python:3.11.3-slim@sha256:eaee5f73efa9ae962d2077756292bc4878c04fcbc13dc168bb00cc365f35647e AS parser
FROM golang:1.23.3-bookworm@sha256:53a9c8dafa550eb772ff742e0a8b80a5cfabd7fdb7635fe0e5e5fa413c85a848
COPY --from=parser /usr/local /usr/local
RUN ldconfig && /usr/local/bin/python3.11 -I -S -B -c "import ast,base64,json,resource,sys; assert sys.implementation.name == 'cpython'; assert sys.version_info[:3] == (3,11,3); ast.parse('return 1')" && /usr/bin/python3 -I -S -B -c "import sys; assert sys.version_info[:3] == (3,11,2)"
