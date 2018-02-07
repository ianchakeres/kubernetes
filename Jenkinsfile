pipeline {
    agent any
//    cleanWs()
    stages {
//    	stage("all") {
//	parallel {
    	stage('make') {
            steps {
                sh "make"
            }
        }
//        stage('make test') {
//            steps {
//                sh "make test"
//            }
//        }
//        stage('make cross') {
//            steps {
//                sh "make cross"
//            }
//        }
//        stage('make quick-release') {
//            steps {
//                sh "make quick-release"
//            }
//        }
//	}
//	}
    }
   	post {
        always {
            echo 'One way or another, I have finished'
            deleteDir() /* clean up our workspace */
        }
	}

}
