# Copyright 2017 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

FROM python:2.7-slim

ENV APP_ROOT=/opt/microservices
ENV PATH=$PATH:$APP_ROOT

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY test-requirements.txt ./
RUN pip install --no-cache-dir -r test-requirements.txt

RUN mkdir ${APP_ROOT} ${APP_ROOT}/templates
COPY productpage.py ${APP_ROOT}
COPY uid_entrypoint ${APP_ROOT}
COPY tests/unit/* ${APP_ROOT}
COPY templates ${APP_ROOT}/templates
COPY requirements.txt ${APP_ROOT}

RUN chgrp -R 0 ${APP_ROOT} && \
    chmod -R og=rwx ${APP_ROOT} && \
    chmod -R g=rw /etc/passwd

EXPOSE 9080
USER 10001
WORKDIR ${APP_ROOT}

RUN python -m unittest discover
ENTRYPOINT [ "uid_entrypoint" ]
CMD python productpage.py 9080
